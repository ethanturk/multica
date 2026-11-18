package uitest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopbackProxyRejectsExternalHTTPAndCONNECTWithoutDialing(t *testing.T) {
	var dials atomic.Int32
	proxy, err := startLoopbackForwardProxy(func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("unexpected dial")
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := proxy.Close(); err != nil {
			t.Errorf("close proxy: %v", err)
		}
	})

	for _, request := range []string{
		"GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n",
		"GET http://10.0.0.1/ HTTP/1.1\r\nHost: 10.0.0.1\r\nConnection: close\r\n\r\n",
		"GET http://[2001:db8::1]/ HTTP/1.1\r\nHost: [2001:db8::1]\r\nConnection: close\r\n\r\n",
		"GET http://[::ffff:8.8.8.8]/ HTTP/1.1\r\nHost: [::ffff:8.8.8.8]\r\nConnection: close\r\n\r\n",
		"CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n",
		"GET ws://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n",
	} {
		response := sendProxyRequest(t, proxy.URL(), request)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s response status = %d, want 403", strings.Fields(request)[0], response.StatusCode)
		}
		_ = response.Body.Close()
	}
	if got := dials.Load(); got != 0 {
		t.Fatalf("external requests made %d dials, want zero", got)
	}
}

func TestLoopbackProxyForwardsHTTPWithoutDNS(t *testing.T) {
	requestPaths := make(chan string, 1)
	requestHeaders := make(chan http.Header, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestPaths <- request.URL.Path
		requestHeaders <- request.Header.Clone()
		writer.Header().Set("Connection", "X-Remove-Response")
		writer.Header().Set("X-Remove-Response", "secret")
		_, _ = io.WriteString(writer, "loopback")
	}))
	defer backend.Close()

	proxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return proxyURL, nil },
	}}
	request, err := http.NewRequest(
		http.MethodGet,
		strings.Replace(backend.URL, "127.0.0.1", "localhost", 1)+"/ok",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Connection", "X-Remove-Request")
	request.Header.Set("X-Remove-Request", "secret")
	request.Header.Set("Proxy-Connection", "keep-alive")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "loopback" {
		t.Fatalf("backend response = %q, want loopback", body)
	}
	if path := <-requestPaths; path != "/ok" {
		t.Fatalf("backend path = %q, want /ok", path)
	}
	headers := <-requestHeaders
	if headers.Get("Connection") != "" || headers.Get("X-Remove-Request") != "" ||
		headers.Get("Proxy-Connection") != "" {
		t.Fatalf("hop-by-hop request headers reached backend: %v", headers)
	}
	if response.Header.Get("Connection") != "" || response.Header.Get("X-Remove-Response") != "" {
		t.Fatalf("hop-by-hop response headers reached client: %v", response.Header)
	}
}

func TestLoopbackProxyForwardsCONNECTTunnel(t *testing.T) {
	backend := startEchoServer(t)
	proxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	connection := dialProxy(t, proxy.URL())
	defer connection.Close()
	_, _ = fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", response.StatusCode)
	}
	_ = response.Body.Close()
	if _, err := io.WriteString(connection, "tunnel"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("tunnel"))
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "tunnel" {
		t.Fatalf("tunnel response = %q", buffer)
	}
}

func TestLoopbackProxyForwardsHTTPUpgrade(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		request, readErr := http.ReadRequest(bufio.NewReader(connection))
		if readErr != nil {
			return
		}
		_ = request.Body.Close()
		_, _ = io.WriteString(connection,
			"HTTP/1.1 101 Switching Protocols\r\n"+
				"Connection: Upgrade, X-Remove-Response\r\n"+
				"Upgrade: websocket\r\n"+
				"Proxy-Connection: keep-alive\r\n"+
				"Keep-Alive: timeout=5\r\n"+
				"X-Remove-Response: secret\r\n\r\n")
		_, _ = io.Copy(connection, connection)
	}()

	proxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	connection := dialProxy(t, proxy.URL())
	defer connection.Close()
	_, _ = fmt.Fprintf(connection,
		"GET ws://%s/socket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n",
		listener.Addr(), listener.Addr())
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", response.StatusCode)
	}
	if response.Header.Get("Connection") != "Upgrade" ||
		response.Header.Get("Upgrade") != "websocket" ||
		response.Header.Get("Proxy-Connection") != "" ||
		response.Header.Get("Keep-Alive") != "" ||
		response.Header.Get("X-Remove-Response") != "" {
		t.Fatalf("unsafe upgrade response headers reached client: %v", response.Header)
	}
	if _, err := io.WriteString(connection, "upgrade"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("upgrade"))
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "upgrade" {
		t.Fatalf("upgrade response = %q", buffer)
	}
	_ = response.Body.Close()
}

func TestLoopbackProxyRejectsConnectionTrackedAfterClose(t *testing.T) {
	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	serverConnection := make(chan net.Conn, 1)
	proxy, err := startLoopbackForwardProxy(func(context.Context, string, string) (net.Conn, error) {
		close(dialStarted)
		<-releaseDial
		client, server := net.Pipe()
		serverConnection <- server
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dialResult := make(chan error, 1)
	go func() {
		connection, dialErr := proxy.dialLoopback(context.Background(), "tcp", "127.0.0.1:80")
		if connection != nil {
			_ = connection.Close()
		}
		dialResult <- dialErr
	}()
	<-dialStarted
	if err := proxy.Close(); err != nil {
		t.Fatal(err)
	}
	close(releaseDial)
	if err := <-dialResult; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("late dial error = %v, want closed proxy", err)
	}
	server := <-serverConnection
	defer server.Close()
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection returned after proxy close remained open")
	}
}

func TestLoopbackProxyCloseTerminatesActiveTunnel(t *testing.T) {
	backend := startEchoServer(t)
	proxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	connection := dialProxy(t, proxy.URL())
	defer connection.Close()
	_, _ = fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if err := proxy.Close(); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("active tunnel remained open after proxy close")
	}
	if _, err := net.DialTimeout("tcp", strings.TrimPrefix(proxy.URL(), "http://"), 50*time.Millisecond); err == nil {
		t.Fatal("proxy listener accepted connection after close")
	}
}

func TestIgnoreClosedNetworkErrorPreservesOnlyRealFailures(t *testing.T) {
	wrappedClosed := &net.OpError{
		Op:  "close",
		Net: "tcp",
		Err: net.ErrClosed,
	}
	if err := ignoreClosedNetworkError(wrappedClosed); err != nil {
		t.Fatalf("wrapped closed error = %v, want nil", err)
	}

	realFailure := errors.New("injected close failure")
	if err := ignoreClosedNetworkError(realFailure); !errors.Is(err, realFailure) {
		t.Fatalf("real close error = %v, want preserved failure", err)
	}
}

func sendProxyRequest(t *testing.T, proxyURL, request string) *http.Response {
	t.Helper()
	connection := dialProxy(t, proxyURL)
	t.Cleanup(func() { _ = connection.Close() })
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func dialProxy(t *testing.T, proxyURL string) net.Conn {
	t.Helper()
	connection, err := net.Dial("tcp", strings.TrimPrefix(proxyURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	return listener.Addr().String()
}
