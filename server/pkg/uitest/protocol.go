package uitest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	maxRPCFrameBytes   = 2 * 1024 * 1024
	rpcDiagnosticBytes = 64 * 1024

	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
	rpcUpstreamError  = -32000
	rpcTimeoutError   = -32001
)

var errRPCFrameTooLarge = errors.New("JSON-RPC frame exceeds 2 MiB")
var errInvalidRPCRequest = errors.New("invalid JSON-RPC request")
var errMalformedRPCJSON = errors.New("malformed JSON-RPC JSON")

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErrorData struct {
	Class  string          `json:"class"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

type rpcError struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    rpcErrorData `json:"data"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func decodeRPCRequest(frame []byte) (rpcRequest, error) {
	if !json.Valid(frame) {
		return rpcRequest{}, errMalformedRPCJSON
	}
	members, err := decodeJSONObject(frame)
	if err != nil {
		return rpcRequest{}, errInvalidRPCRequest
	}
	for name := range members {
		switch name {
		case "jsonrpc", "id", "method", "params":
		default:
			return rpcRequest{}, errInvalidRPCRequest
		}
	}
	var request rpcRequest
	if err := json.Unmarshal(members["jsonrpc"], &request.JSONRPC); err != nil ||
		request.JSONRPC != "2.0" {
		return rpcRequest{}, errInvalidRPCRequest
	}
	if err := json.Unmarshal(members["method"], &request.Method); err != nil ||
		request.Method == "" {
		return rpcRequest{}, errInvalidRPCRequest
	}
	if id, ok := members["id"]; ok {
		if !validRPCID(id) {
			return rpcRequest{}, errInvalidRPCRequest
		}
		request.ID = append(json.RawMessage(nil), id...)
	}
	if params, ok := members["params"]; ok {
		trimmed := bytes.TrimSpace(params)
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			return rpcRequest{}, errInvalidRPCRequest
		}
		request.Params = append(json.RawMessage(nil), params...)
	}
	return request, nil
}

func decodeJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errInvalidRPCRequest
	}
	members := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errInvalidRPCRequest
		}
		name, ok := token.(string)
		if !ok {
			return nil, errInvalidRPCRequest
		}
		if _, duplicate := members[name]; duplicate {
			return nil, errInvalidRPCRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errInvalidRPCRequest
		}
		members[name] = append(json.RawMessage(nil), value...)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, errInvalidRPCRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errInvalidRPCRequest
	}
	return members, nil
}

func validRPCID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '"' {
		var value string
		return json.Unmarshal(trimmed, &value) == nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return false
	}
	return number.String() != ""
}

func readRPCFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !oversized {
			if len(frame)+len(fragment) > maxRPCFrameBytes+1 {
				oversized = true
			} else {
				frame = append(frame, fragment...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if errors.Is(err, io.EOF) && len(fragment) == 0 && len(frame) == 0 && !oversized {
			return nil, io.EOF
		}
		if oversized {
			return nil, errRPCFrameTooLarge
		}
		frame = bytes.TrimSpace(frame)
		if len(frame) > maxRPCFrameBytes {
			return nil, errRPCFrameTooLarge
		}
		if len(frame) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return frame, nil
		}
		return frame, nil
	}
}

func writeRPCFrame(writer io.Writer, value any) error {
	frame, err := marshalRPCFrame(value)
	if err != nil {
		return err
	}
	written, err := writer.Write(frame)
	if err == nil && written != len(frame) {
		err = io.ErrShortWrite
	}
	return err
}

func marshalRPCFrame(value any) ([]byte, error) {
	frame, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON-RPC frame: %w", err)
	}
	if len(frame) > maxRPCFrameBytes {
		return nil, errRPCFrameTooLarge
	}
	frame = append(frame, '\n')
	return frame, nil
}

func boundedRPCString(value string) string {
	if len(value) <= rpcDiagnosticBytes {
		return value
	}
	return value[:rpcDiagnosticBytes] + "...[truncated]"
}

func responseError(id json.RawMessage, code int, class, message string) rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), id...),
		Error: &rpcError{
			Code:    code,
			Message: boundedRPCString(message),
			Data:    rpcErrorData{Class: class},
		},
	}
}

func boundedUpstreamResponse(response rpcResponse, id json.RawMessage) rpcResponse {
	response.JSONRPC = "2.0"
	response.ID = append(json.RawMessage(nil), id...)
	if response.Error == nil {
		if len(response.Result) > maxRPCFrameBytes-rpcDiagnosticBytes {
			return responseError(id, rpcUpstreamError, ErrorBrowser, "upstream response exceeded safe output limit")
		}
		return response
	}
	response.Error.Message = boundedRPCString(response.Error.Message)
	response.Error.Data = rpcErrorData{Class: ErrorBrowser}
	if len(mustMarshalRPC(response)) > maxRPCFrameBytes {
		return responseError(id, rpcUpstreamError, ErrorBrowser, "upstream error exceeded safe output limit")
	}
	return response
}

func mustMarshalRPC(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
