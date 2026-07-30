package uitest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const loopbackProxyHeaderTimeout = 30 * time.Second

type loopbackDialFunc func(context.Context, string, string) (net.Conn, error)

type loopbackForwardProxy struct {
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
	dial      loopbackDialFunc

	mu          sync.Mutex
	connections map[net.Conn]struct{}
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

func startLoopbackForwardProxy(dial loopbackDialFunc) (*loopbackForwardProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for UI test network proxy: %w", err)
	}
	if dial == nil {
		networkDialer := &net.Dialer{}
		dial = networkDialer.DialContext
	}
	proxy := &loopbackForwardProxy{
		listener:    listener,
		dial:        dial,
		connections: make(map[net.Conn]struct{}),
	}
	proxy.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           proxy.dialLoopback,
		ForceAttemptHTTP2:     false,
		DisableCompression:    true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   16,
		ResponseHeaderTimeout: loopbackProxyHeaderTimeout,
	}
	proxy.server = &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: loopbackProxyHeaderTimeout,
	}
	go func() {
		_ = proxy.server.Serve(&trackingListener{Listener: listener, proxy: proxy})
	}()
	return proxy, nil
}

func (p *loopbackForwardProxy) URL() string {
	return "http://" + p.listener.Addr().String()
}

func (p *loopbackForwardProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodConnect {
		p.serveCONNECT(writer, request)
		return
	}
	if request.URL == nil || !request.URL.IsAbs() ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https" &&
			request.URL.Scheme != "ws" && request.URL.Scheme != "wss") {
		http.Error(writer, "UI test proxy requires an absolute HTTP URL", http.StatusBadRequest)
		return
	}
	address, err := loopbackURLAddress(request.URL)
	if err != nil || validateLoopbackAddress(address) != nil {
		http.Error(writer, "UI test browser network policy blocked destination", http.StatusForbidden)
		return
	}
	outbound := request.Clone(request.Context())
	targetURL := *request.URL
	outbound.URL = &targetURL
	switch outbound.URL.Scheme {
	case "ws":
		outbound.URL.Scheme = "http"
	case "wss":
		outbound.URL.Scheme = "https"
	}
	outbound.RequestURI = ""
	outbound.Header = request.Header.Clone()
	stripProxyHopHeaders(outbound.Header, isProxyUpgrade(request.Header))
	response, err := p.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(writer, "UI test proxy could not reach loopback destination", http.StatusBadGateway)
		return
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		p.forwardUpgrade(writer, response)
		return
	}
	defer response.Body.Close()
	stripProxyHopHeaders(response.Header, false)
	copyProxyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (p *loopbackForwardProxy) serveCONNECT(writer http.ResponseWriter, request *http.Request) {
	if validateLoopbackAddress(request.Host) != nil {
		http.Error(writer, "UI test browser network policy blocked destination", http.StatusForbidden)
		return
	}
	upstream, err := p.dialLoopback(request.Context(), "tcp", request.Host)
	if err != nil {
		http.Error(writer, "UI test proxy could not reach loopback destination", http.StatusBadGateway)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(writer, "UI test proxy tunnel unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	if _, err := io.WriteString(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	if err := buffered.Flush(); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	p.tunnel(&bufferedProxyConn{Conn: client, reader: buffered.Reader}, upstream)
}

func (p *loopbackForwardProxy) forwardUpgrade(writer http.ResponseWriter, response *http.Response) {
	upstream, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		_ = response.Body.Close()
		http.Error(writer, "UI test proxy upgrade unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(writer, "UI test proxy upgrade unavailable", http.StatusInternalServerError)
		return
	}
	stripProxyHopHeaders(response.Header, true)
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	if _, err := fmt.Fprintf(buffered, "HTTP/1.1 %s\r\n", response.Status); err == nil {
		err = response.Header.Write(buffered)
	}
	if err == nil {
		_, err = io.WriteString(buffered, "\r\n")
	}
	if err == nil {
		err = buffered.Flush()
	}
	if err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	p.tunnel(&bufferedProxyConn{Conn: client, reader: buffered.Reader}, upstream)
}

func (p *loopbackForwardProxy) tunnel(left, right io.ReadWriteCloser) {
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, 2)
	copySide := func(destination io.Writer, source io.Reader) {
		_, _ = io.Copy(destination, source)
		done <- struct{}{}
	}
	go copySide(left, right)
	go copySide(right, left)
	<-done
	_ = left.Close()
	_ = right.Close()
	<-done
}

func (p *loopbackForwardProxy) dialLoopback(
	ctx context.Context,
	_ string,
	address string,
) (net.Conn, error) {
	candidates, err := loopbackDialAddresses(address)
	if err != nil {
		return nil, err
	}
	var dialErr error
	for _, candidate := range candidates {
		connection, err := p.dial(ctx, "tcp", candidate)
		if err == nil {
			return p.track(connection)
		}
		dialErr = errors.Join(dialErr, err)
	}
	return nil, dialErr
}

func validateLoopbackAddress(address string) error {
	_, err := loopbackDialAddresses(address)
	return err
}

func loopbackDialAddresses(address string) ([]string, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid destination address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid destination port")
	}
	if strings.EqualFold(host, "localhost") {
		return []string{
			net.JoinHostPort("127.0.0.1", portText),
			net.JoinHostPort("::1", portText),
		}, nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("destination is not loopback")
	}
	return []string{net.JoinHostPort(ip.String(), portText)}, nil
}

func loopbackURLAddress(target *url.URL) (string, error) {
	host := target.Hostname()
	if host == "" {
		return "", fmt.Errorf("destination host is required")
	}
	port := target.Port()
	if port == "" {
		switch target.Scheme {
		case "http", "ws":
			port = "80"
		case "https", "wss":
			port = "443"
		default:
			return "", fmt.Errorf("unsupported destination scheme")
		}
	}
	return net.JoinHostPort(host, port), nil
}

func validateLoopbackProxyServer(raw string) error {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "http" || target.User != nil ||
		(target.Path != "" && target.Path != "/") ||
		target.RawQuery != "" || target.Fragment != "" {
		return fmt.Errorf("invalid loopback HTTP proxy URL")
	}
	address, err := loopbackURLAddress(target)
	if err != nil {
		return err
	}
	return validateLoopbackAddress(address)
}

func (p *loopbackForwardProxy) track(connection net.Conn) (net.Conn, error) {
	tracked := &trackedProxyConn{Conn: connection, proxy: p}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = connection.Close()
		return nil, net.ErrClosed
	}
	p.connections[tracked] = struct{}{}
	p.mu.Unlock()
	return tracked, nil
}

func (p *loopbackForwardProxy) untrack(connection net.Conn) {
	p.mu.Lock()
	delete(p.connections, connection)
	p.mu.Unlock()
}

func (p *loopbackForwardProxy) Close() error {
	p.closeOnce.Do(func() {
		p.transport.CloseIdleConnections()
		p.mu.Lock()
		p.closed = true
		connections := make([]net.Conn, 0, len(p.connections))
		for connection := range p.connections {
			connections = append(connections, connection)
		}
		p.mu.Unlock()
		listenerErr := ignoreClosedNetworkError(p.listener.Close())
		serverErr := ignoreClosedNetworkError(p.server.Close())
		var connectionErr error
		for _, connection := range connections {
			if err := ignoreClosedNetworkError(connection.Close()); err != nil {
				connectionErr = errors.Join(connectionErr, err)
			}
		}
		p.closeErr = errors.Join(listenerErr, serverErr, connectionErr)
	})
	return p.closeErr
}

func ignoreClosedNetworkError(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

type trackingListener struct {
	net.Listener
	proxy *loopbackForwardProxy
}

func (l *trackingListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.proxy.track(connection)
}

type trackedProxyConn struct {
	net.Conn
	proxy    *loopbackForwardProxy
	once     sync.Once
	closeErr error
}

func (c *trackedProxyConn) Close() error {
	c.once.Do(func() {
		c.closeErr = c.Conn.Close()
		if errors.Is(c.closeErr, net.ErrClosed) {
			c.closeErr = nil
		}
		c.proxy.untrack(c)
	})
	return c.closeErr
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(buffer []byte) (int, error) {
	return c.reader.Read(buffer)
}

func copyProxyHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func isProxyUpgrade(header http.Header) bool {
	return header.Get("Upgrade") != "" && headerHasToken(header, "Connection", "upgrade")
}

func stripProxyHopHeaders(header http.Header, preserveUpgrade bool) {
	for _, connectionValue := range header.Values("Connection") {
		for _, name := range strings.Split(connectionValue, ",") {
			name = strings.TrimSpace(name)
			if name != "" && !(preserveUpgrade && strings.EqualFold(name, "upgrade")) {
				header.Del(name)
			}
		}
	}
	for _, name := range []string{
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
	} {
		header.Del(name)
	}
	if preserveUpgrade {
		header.Set("Connection", "Upgrade")
		return
	}
	header.Del("Connection")
	header.Del("Upgrade")
}

func headerHasToken(header http.Header, name, want string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), want) {
				return true
			}
		}
	}
	return false
}
