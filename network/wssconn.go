package network

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WssConfig configures TLS and the exact HTTP upgrade path. Configure before use;
// do not mutate TLSConfig or GetConfig's result concurrently with Dial/Listen.
// TLSConfig supports RootCAs, ServerName, Certificates and optional mutual TLS.
// InsecureSkipVerify is rejected. Listen requires a certificate or GetCertificate.
type WssConfig struct {
	TLSConfig        *tls.Config
	Path             string
	HandshakeTimeout time.Duration
	AcceptChanLen    int
}

func DefaultWssConfig() *WssConfig {
	return &WssConfig{Path: "/gohome", HandshakeTimeout: 10 * time.Second, AcceptChanLen: 128}
}

// WssConn tunnels a binary byte stream over verified TLS WebSockets. It is a
// factory until Dial/Listen returns a new connection. Concurrent reads/writes
// are serialized by websocket.NetConn. As with that adapter, an active deadline
// expiry closes the stream permanently; Close aborts pending IO immediately.
type WssConn struct {
	readEOF       bool
	readDeadline  time.Time
	writeDeadline time.Time
	mu            sync.Mutex
	config        *WssConfig
	closed        bool
	pending       map[*context.CancelFunc]struct{}
	stream        net.Conn
	ws            *websocket.Conn
	listener      *wssListener
	parent        *wssListener
}

type wssListener struct {
	mu       sync.Mutex
	closed   bool
	ln       net.Listener
	srv      *http.Server
	done     chan struct{}
	served   chan struct{}
	accept   chan *WssConn
	children map[*WssConn]struct{}
}

func (c *WssConn) Name() string             { return "wss" }
func (c *WssConn) SetConfig(cfg *WssConfig) { c.mu.Lock(); c.config = cfg; c.mu.Unlock() }
func (c *WssConn) GetConfig() *WssConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultWssConfig()
	}
	return c.config
}
func (c *WssConn) snapshot() (*WssConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	cfg := DefaultWssConfig()
	if c.config != nil {
		*cfg = *c.config
	}
	if cfg.Path == "" {
		cfg.Path = "/gohome"
	}
	u, err := url.Parse(cfg.Path)
	if err != nil || !strings.HasPrefix(cfg.Path, "/") || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Path != cfg.Path {
		return nil, errors.New("wss: Path must be an absolute, unescaped URL path")
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 10 * time.Second
	}
	if cfg.AcceptChanLen <= 0 {
		cfg.AcceptChanLen = 128
	}
	if cfg.TLSConfig == nil {
		cfg.TLSConfig = &tls.Config{}
	} else {
		cfg.TLSConfig = cfg.TLSConfig.Clone()
	}
	if cfg.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("wss: InsecureSkipVerify is forbidden")
	}
	if cfg.TLSConfig.MinVersion < tls.VersionTLS12 {
		cfg.TLSConfig.MinVersion = tls.VersionTLS12
	}
	// WebSocket Upgrade uses HTTP/1.1, including through reverse proxies.
	cfg.TLSConfig.NextProtos = []string{"http/1.1"}
	return cfg, nil
}

func (c *WssConn) Dial(dst string) (Conn, error) { return c.DialContext(context.Background(), dst) }

// DialContext bounds the handshake only, like net.Dialer.DialContext. Close on
// the factory cancels all pending handshakes, not previously returned streams.
func (c *WssConn) DialContext(ctx context.Context, dst string) (Conn, error) {
	cfg, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	if !strings.Contains(dst, "://") {
		dst = "wss://" + dst
	}
	u, err := url.Parse(dst)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("wss: expected wss://host[:port][/path]")
	}
	if u.Path == "" {
		u.Path = cfg.Path
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.HandshakeTimeout)
	defer cancel()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, net.ErrClosed
	}
	if c.pending == nil {
		c.pending = make(map[*context.CancelFunc]struct{})
	}
	c.pending[&cancel] = struct{}{}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, &cancel); c.mu.Unlock() }()
	d := &net.Dialer{Control: gControlOnConnSetup}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: cfg.TLSConfig, DialContext: d.DialContext, TLSHandshakeTimeout: cfg.HandshakeTimeout}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("wss: redirects are not allowed") }}
	ws, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: client, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		_ = ws.CloseNow()
		return nil, net.ErrClosed
	}
	return newWssStream(ws, nil), nil
}

func newWssStream(ws *websocket.Conn, parent *wssListener) *WssConn {
	return &WssConn{ws: ws, stream: websocket.NetConn(context.Background(), ws, websocket.MessageBinary), parent: parent}
}

func (c *WssConn) Listen(dst string) (Conn, error) {
	cfg, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	if len(cfg.TLSConfig.Certificates) == 0 && cfg.TLSConfig.GetCertificate == nil {
		return nil, errors.New("wss: Listen requires a TLS certificate")
	}
	ln, err := tls.Listen("tcp", dst, cfg.TLSConfig)
	if err != nil {
		return nil, err
	}
	l := &wssListener{ln: ln, done: make(chan struct{}), served: make(chan struct{}), accept: make(chan *WssConn, cfg.AcceptChanLen), children: make(map[*WssConn]struct{})}
	l.srv = &http.Server{ReadHeaderTimeout: cfg.HandshakeTimeout, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cfg.Path {
			http.NotFound(w, r)
			return
		}
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		child := newWssStream(ws, l)
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			_ = child.Close()
			return
		}
		l.children[child] = struct{}{}
		select {
		case l.accept <- child:
			l.mu.Unlock()
		default:
			delete(l.children, child)
			l.mu.Unlock()
			_ = child.Close()
		}
	})}
	go func() { defer close(l.served); _ = l.srv.Serve(ln) }()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	result := &WssConn{listener: l}
	if closed {
		_ = result.Close()
		return nil, net.ErrClosed
	}
	return result, nil
}
func (c *WssConn) Accept() (Conn, error) {
	l := c.listener
	if l == nil {
		return nil, errors.New("wss: not a listener")
	}
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case child := <-l.accept:
		l.mu.Lock()
		closed := l.closed
		l.mu.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		return child, nil
	}
}
func (c *WssConn) ioState(write bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	deadline := c.readDeadline
	if write {
		deadline = c.writeDeadline
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return os.ErrDeadlineExceeded
	}
	return nil
}
func (c *WssConn) finishIO(err error, write bool) error {
	if err == nil {
		return nil
	}
	if !write && errors.Is(err, io.EOF) {
		c.mu.Lock()
		c.readEOF = true
		c.mu.Unlock()
	}
	state := c.ioState(write)
	_ = c.Close()
	if errors.Is(state, os.ErrDeadlineExceeded) {
		return state
	}
	return err
}
func (c *WssConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	eof := c.readEOF
	c.mu.Unlock()
	if eof {
		return 0, io.EOF
	}
	if c.stream == nil {
		return 0, errors.New("wss: not a stream")
	}
	if err := c.ioState(false); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for {
		n, err := c.stream.Read(p)
		if n != 0 || err != nil {
			return n, c.finishIO(err, false)
		}
	}
}
func (c *WssConn) Write(p []byte) (int, error) {
	if c.stream == nil {
		return 0, errors.New("wss: not a stream")
	}
	if err := c.ioState(true); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err := c.stream.Write(p)
	return n, c.finishIO(err, true)
}
func (c *WssConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for cancel := range c.pending {
		(*cancel)()
	}
	c.mu.Unlock()
	if c.ws != nil {
		// CloseNow avoids the close handshake's wait when the peer isn't reading.
		_ = c.ws.CloseNow()
		if c.parent != nil {
			c.parent.mu.Lock()
			delete(c.parent.children, c)
			c.parent.mu.Unlock()
		}
	}
	if l := c.listener; l != nil {
		l.mu.Lock()
		l.closed = true
		close(l.done)
		children := make([]*WssConn, 0, len(l.children))
		for child := range l.children {
			children = append(children, child)
		}
		l.mu.Unlock()
		_ = l.srv.Close()
		for _, child := range children {
			_ = child.Close()
		}
		<-l.served
	}
	return nil
}
func (c *WssConn) Info() string {
	if c.listener != nil {
		return "wss listener--" + c.listener.ln.Addr().String()
	}
	if c.stream != nil {
		return "wss stream--" + c.stream.RemoteAddr().String()
	}
	return "empty wss conn"
}

// Addr returns the actual listening address, including an OS-selected port.
func (c *WssConn) Addr() net.Addr {
	if c.listener != nil {
		return c.listener.ln.Addr()
	}
	if c.stream != nil {
		return c.stream.LocalAddr()
	}
	return nil
}
func (c *WssConn) SetDeadline(t time.Time) error {
	if c.stream == nil {
		return errors.New("wss: not a stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	c.readDeadline = t
	c.writeDeadline = t
	return c.stream.SetDeadline(t)
}
func (c *WssConn) SetReadDeadline(t time.Time) error {
	if c.stream == nil {
		return errors.New("wss: not a stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	c.readDeadline = t
	return c.stream.SetReadDeadline(t)
}
func (c *WssConn) SetWriteDeadline(t time.Time) error {
	if c.stream == nil {
		return errors.New("wss: not a stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	c.writeDeadline = t
	return c.stream.SetWriteDeadline(t)
}
