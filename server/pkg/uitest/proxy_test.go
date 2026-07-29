package uitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type fakeProxySession struct {
	mu      sync.Mutex
	actions int
	closes  int
	err     error
}

func (s *fakeProxySession) RunBrowserAction(ctx context.Context, action func(context.Context) error) error {
	s.mu.Lock()
	s.actions++
	s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	return action(ctx)
}

func (s *fakeProxySession) actionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actions
}

func (s *fakeProxySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func (s *fakeProxySession) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

type fakeProxyUpstream struct {
	mu            sync.Mutex
	requests      []rpcRequest
	notifications []rpcRequest
	responses     map[string]rpcResponse
	events        chan rpcRequest
	done          chan struct{}
	terminalErr   error
	closes        int
	eventClose    sync.Once
	doneClose     sync.Once
}

func newFakeProxyUpstream() *fakeProxyUpstream {
	return &fakeProxyUpstream{
		responses: map[string]rpcResponse{},
		events:    make(chan rpcRequest),
		done:      make(chan struct{}),
	}
}

func (u *fakeProxyUpstream) request(_ context.Context, request rpcRequest) (rpcResponse, error) {
	u.mu.Lock()
	u.requests = append(u.requests, request)
	response, ok := u.responses[request.Method]
	u.mu.Unlock()
	if !ok {
		response = rpcResponse{JSONRPC: "2.0", Result: json.RawMessage(`{}`)}
	}
	return response, nil
}

func (u *fakeProxyUpstream) notify(_ context.Context, request rpcRequest) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.notifications = append(u.notifications, request)
	return nil
}

func (u *fakeProxyUpstream) eventStream() <-chan rpcRequest { return u.events }
func (u *fakeProxyUpstream) doneStream() <-chan struct{}    { return u.done }

func (u *fakeProxyUpstream) terminalError() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.terminalErr
}

func (u *fakeProxyUpstream) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closes++
	return nil
}

func (u *fakeProxyUpstream) closeCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closes
}

func (u *fakeProxyUpstream) crash(err error) {
	u.mu.Lock()
	u.terminalErr = err
	u.mu.Unlock()
	u.eventClose.Do(func() { close(u.events) })
	u.doneClose.Do(func() { close(u.done) })
}

func (u *fakeProxyUpstream) closeEvents() {
	u.eventClose.Do(func() { close(u.events) })
}

func (u *fakeProxyUpstream) calls() []rpcRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]rpcRequest(nil), u.requests...)
}

func TestProxyHandshakeListAndAllowedCall(t *testing.T) {
	upstream := newFakeProxyUpstream()
	upstream.responses["initialize"] = rpcResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake"}}`),
	}
	upstream.responses["tools/list"] = rpcResponse{
		JSONRPC: "2.0",
		Result: json.RawMessage(`{"tools":[
			{"name":"browser_click","description":"click","inputSchema":{"type":"object"}},
			{"name":"browser_evaluate","description":"unsafe","inputSchema":{"type":"object"}},
			{"name":"future_browser_tool","description":"unknown","inputSchema":{"type":"object"}}
		]}`),
	}
	upstream.responses["tools/call"] = rpcResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"content":[{"type":"text","text":"clicked"}]}`),
	}
	session := &fakeProxySession{}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"browser_click","arguments":{"element":"Save","ref":"e1"}}}`,
	}, "\n") + "\n"

	responses := runProxy(t, session, upstream, []byte("fixed axe"), input)
	if len(responses) != 4 {
		t.Fatalf("response count = %d, want 4", len(responses))
	}
	if got := string(responses[0].ID); got != `"init"` {
		t.Fatalf("initialize ID = %s, want exact string ID", got)
	}
	if got := string(responses[1].ID); got != "2" {
		t.Fatalf("ping ID = %s, want 2", got)
	}

	var listed struct {
		Tools []toolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(responses[2].Result, &listed); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "browser_click,browser_accessibility_scan" {
		t.Fatalf("listed tools = %v, want safe intersection plus fixed Axe", names)
	}
	if session.actionCount() != 1 {
		t.Fatalf("browser actions = %d, want one", session.actionCount())
	}
	calls := upstream.calls()
	if len(calls) != 4 {
		t.Fatalf("upstream request count = %d, want initialize, ping, list, call", len(calls))
	}
	upstream.mu.Lock()
	notifications := append([]rpcRequest(nil), upstream.notifications...)
	upstream.mu.Unlock()
	if len(notifications) != 1 || notifications[0].Method != "notifications/initialized" {
		t.Fatalf("upstream notifications = %#v", notifications)
	}
}

func TestProxyToolsListStartsNeitherApplicationNorBrowser(t *testing.T) {
	upstream := newFakeProxyUpstream()
	upstream.responses["tools/list"] = rpcResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"tools":[{"name":"browser_snapshot","inputSchema":{"type":"object"}}]}`),
	}
	session := &fakeProxySession{}
	responses := runProxy(t, session, upstream, nil,
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n")
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("tools/list response = %#v", responses)
	}
	if session.actionCount() != 0 {
		t.Fatalf("browser actions = %d, want zero", session.actionCount())
	}
}

func TestProxyDefaultDeniesHiddenUnknownAndUnsupportedMethods(t *testing.T) {
	for _, request := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"browser_evaluate","arguments":{"function":"() => process.env"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"browser_run_code_unsafe","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"future_browser_tool","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
	} {
		t.Run(request, func(t *testing.T) {
			upstream := newFakeProxyUpstream()
			responses := runProxy(t, &fakeProxySession{}, upstream, nil, request+"\n")
			if len(responses) != 1 || responses[0].Error == nil {
				t.Fatalf("response = %#v, want JSON-RPC error", responses)
			}
			if responses[0].Error.Data.Class != ErrorPolicy {
				t.Fatalf("error class = %q, want %q", responses[0].Error.Data.Class, ErrorPolicy)
			}
			if len(upstream.calls()) != 0 {
				t.Fatal("denied request reached upstream")
			}
		})
	}
}

func TestProxyRejectsExternalNavigateBeforeSessionAndUpstream(t *testing.T) {
	upstream := newFakeProxyUpstream()
	session := &fakeProxySession{}
	responses := runProxy(t, session, upstream, nil,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"browser_navigate","arguments":{"url":"https://example.com"}}}`+"\n")
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("response = %#v, want policy error", responses)
	}
	if session.actionCount() != 0 {
		t.Fatal("external navigation initialized session")
	}
	if len(upstream.calls()) != 0 {
		t.Fatal("external navigation reached upstream")
	}
}

func TestProxyNavigateRejectsUnknownDuplicateAndNonStringArguments(t *testing.T) {
	tests := map[string]string{
		"unknown":   `{"url":"http://127.0.0.1:3000","extra":"forwarded"}`,
		"duplicate": `{"url":"http://127.0.0.1:3000","url":"http://localhost:3000"}`,
		"number":    `{"url":3000}`,
		"array":     `{"url":["http://127.0.0.1:3000"]}`,
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			upstream := newFakeProxyUpstream()
			session := &fakeProxySession{}
			responses := runProxy(t, session, upstream, nil,
				`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"browser_navigate","arguments":`+
					arguments+`}}`+"\n")
			if len(responses) != 1 || responses[0].Error == nil ||
				responses[0].Error.Code != rpcInvalidParams {
				t.Fatalf("response = %#v, want invalid params", responses)
			}
			if session.actionCount() != 0 || len(upstream.calls()) != 0 {
				t.Fatal("invalid navigation reached session or upstream")
			}
		})
	}
}

func TestProxyNavigateReconstructsSanitizedUpstreamArguments(t *testing.T) {
	upstream := newFakeProxyUpstream()
	responses := runProxy(t, &fakeProxySession{}, upstream, nil,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"browser_navigate","arguments":{"url":"http://127.0.0.1:3000"}}}`+"\n")
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("response = %#v", responses)
	}
	calls := upstream.calls()
	if len(calls) != 1 {
		t.Fatalf("upstream calls = %d, want one", len(calls))
	}
	var call struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(calls[0].Params, &call); err != nil {
		t.Fatal(err)
	}
	if call.Name != "browser_navigate" || len(call.Arguments) != 1 ||
		string(call.Arguments["url"]) != `"http://127.0.0.1:3000"` {
		t.Fatalf("sanitized call = %#v", call)
	}
}

func TestProxyFirstPermittedCallRunsSessionReadiness(t *testing.T) {
	upstream := newFakeProxyUpstream()
	session := &fakeProxySession{}
	responses := runProxy(t, session, upstream, nil,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"browser_navigate","arguments":{"url":"http://127.0.0.1:3000"}}}`+"\n")
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("response = %#v", responses)
	}
	if session.actionCount() != 1 {
		t.Fatalf("session actions = %d, want 1", session.actionCount())
	}
}

func TestProxyReturnsInvalidRequestForValidJSONWithInvalidEnvelope(t *testing.T) {
	tests := map[string]string{
		"empty object":             `{}`,
		"wrong version":            `{"jsonrpc":"1.0","method":"ping"}`,
		"boolean id":               `{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		"object id":                `{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		"array id":                 `{"jsonrpc":"2.0","id":[],"method":"ping"}`,
		"duplicate id":             `{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`,
		"duplicate method":         `{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/list"}`,
		"non-string method":        `{"jsonrpc":"2.0","id":1,"method":7}`,
		"array envelope":           `[]`,
		"scalar params":            `{"jsonrpc":"2.0","id":1,"method":"ping","params":true}`,
		"request method no id":     `{"jsonrpc":"2.0","method":"ping"}`,
		"notification method id":   `{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`,
		"unknown notification":     `{"jsonrpc":"2.0","method":"notifications/unknown"}`,
		"duplicate jsonrpc member": `{"jsonrpc":"2.0","jsonrpc":"2.0","id":1,"method":"ping"}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			upstream := newFakeProxyUpstream()
			responses := runProxy(t, &fakeProxySession{}, upstream, nil, input+"\n")
			if len(responses) != 1 || responses[0].Error == nil ||
				responses[0].Error.Code != rpcInvalidRequest {
				t.Fatalf("responses = %#v, want one -32600 response", responses)
			}
			if string(responses[0].ID) != "null" {
				t.Fatalf("invalid-request ID = %s, want null", responses[0].ID)
			}
			if len(upstream.calls()) != 0 {
				t.Fatal("invalid envelope reached upstream")
			}
		})
	}
}

func TestProxyReturnsParseErrorOnlyForMalformedJSON(t *testing.T) {
	responses := runProxy(t, &fakeProxySession{}, newFakeProxyUpstream(), nil, "{\n")
	if len(responses) != 1 || responses[0].Error == nil ||
		responses[0].Error.Code != rpcParseError ||
		string(responses[0].ID) != "null" {
		t.Fatalf("responses = %#v, want one -32700 response with null ID", responses)
	}
}

func TestProxyForwardsBoundedUpstreamNotification(t *testing.T) {
	upstream := newFakeProxyUpstream()
	clientReader, clientWriter := io.Pipe()
	var output lockedBuffer
	proxy := newProxy(&fakeProxySession{}, upstream, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() {
		done <- proxy.Serve(context.Background(), clientReader, &output)
	}()
	upstream.events <- rpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/message",
		Params:  json.RawMessage(`{"level":"info"}`),
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "notifications/message") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := clientWriter.Close(); err != nil {
		t.Fatal(err)
	}
	upstream.closeEvents()
	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if !strings.Contains(output.String(), `"method":"notifications/message"`) {
		t.Fatalf("output = %q, want upstream notification", output.String())
	}
}

func TestProxyCancellationUnblocksClosableOutputAndClosesOwners(t *testing.T) {
	upstream := newFakeProxyUpstream()
	session := &fakeProxySession{}
	clientReader, clientWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	proxy := newProxy(session, upstream, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- proxy.Serve(ctx, clientReader, outputWriter)
	}()
	if _, err := io.WriteString(clientWriter, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitForProxyCalls(t, upstream, 1)

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve() error = %v, want cancellation", err)
		}
	case <-time.After(200 * time.Millisecond):
		_ = outputReader.Close()
		_ = clientWriter.Close()
		<-done
		t.Fatal("Serve() remained blocked writing to a full client pipe after cancellation")
	}
	if session.closeCount() != 1 || upstream.closeCount() != 1 {
		t.Fatalf("close counts: session=%d upstream=%d, want one each", session.closeCount(), upstream.closeCount())
	}
	_ = outputReader.Close()
	_ = clientWriter.Close()
}

func TestProxyIdleUpstreamEOFTerminatesOpenClientAndClosesOwners(t *testing.T) {
	upstream := newFakeProxyUpstream()
	session := &fakeProxySession{}
	clientReader, clientWriter := io.Pipe()
	proxy := newProxy(session, upstream, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() {
		done <- proxy.Serve(context.Background(), clientReader, io.Discard)
	}()

	upstream.crash(io.EOF)
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Serve() error = %v, want upstream EOF", err)
		}
	case <-time.After(200 * time.Millisecond):
		_ = clientWriter.Close()
		<-done
		t.Fatal("Serve() ignored idle upstream EOF while client input remained open")
	}
	if session.closeCount() != 1 || upstream.closeCount() != 1 {
		t.Fatalf("close counts: session=%d upstream=%d, want one each", session.closeCount(), upstream.closeCount())
	}
	_ = clientWriter.Close()
}

func TestProxyRejectsFrameAboveTwoMiB(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"method":"ping","padding":"` +
		strings.Repeat("x", maxRPCFrameBytes) + `"}` + "\n"
	responses := runProxy(t, &fakeProxySession{}, newFakeProxyUpstream(), nil, payload)
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("responses = %#v, want bounded parse error", responses)
	}
	if len(mustJSON(t, responses[0])) > maxRPCFrameBytes {
		t.Fatal("oversize rejection exceeded frame bound")
	}
}

func TestProxyBoundsAndClassifiesUpstreamErrorsWithoutChangingID(t *testing.T) {
	upstream := newFakeProxyUpstream()
	upstream.responses["tools/call"] = rpcResponse{
		JSONRPC: "2.0",
		Error: &rpcError{
			Code:    -32000,
			Message: strings.Repeat("upstream secret ", 100_000),
		},
	}
	responses := runProxy(t, &fakeProxySession{}, upstream, nil,
		`{"jsonrpc":"2.0","id":"same-id","method":"tools/call","params":{"name":"browser_snapshot","arguments":{}}}`+"\n")
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("response = %#v, want error", responses)
	}
	if string(responses[0].ID) != `"same-id"` {
		t.Fatalf("ID = %s, want exact original ID", responses[0].ID)
	}
	if responses[0].Error.Data.Class != ErrorBrowser {
		t.Fatalf("class = %q, want %q", responses[0].Error.Data.Class, ErrorBrowser)
	}
	if len(responses[0].Error.Message) > rpcDiagnosticBytes+32 {
		t.Fatalf("error message length = %d, want bounded", len(responses[0].Error.Message))
	}
}

func runProxy(
	t *testing.T,
	session browserActionSession,
	upstream proxyUpstream,
	axe []byte,
	input string,
) []rpcResponse {
	t.Helper()
	upstream.(*fakeProxyUpstream).closeEvents()
	var output bytes.Buffer
	proxy := newProxy(session, upstream, axe, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := proxy.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	var responses []rpcResponse
	decoder := json.NewDecoder(&output)
	for {
		var response rpcResponse
		if err := decoder.Decode(&response); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode response: %v\noutput: %s", err, output.String())
		}
		responses = append(responses, response)
	}
	return responses
}

func waitForProxyCalls(t *testing.T, upstream *fakeProxyUpstream, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(upstream.calls()) < count && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(upstream.calls()); got < count {
		t.Fatalf("upstream calls = %d, want at least %d", got, count)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
