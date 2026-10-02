package network

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/quic-go/quic-go"
)

/*
QuicConn 实现了基于 Quic 协议的Conn。
单条 QUIC stream 直接作为读写通道（不再叠 smux）。

A listener fans every stream of every accepted QUIC session out as its own
Conn, so multiple logical connections can share one QUIC session. Dialing
clients reuse a process-wide per-address session cache, opening a new stream
on subsequent Dials instead of repeating the UDP/TLS/QUIC handshake.
*/

// quicSessionIdleTTL is how long an unreferenced client session stays open
// waiting for another Dial to reuse it before it is torn down.
const quicSessionIdleTTL = 30 * time.Second

// quicSessionAcceptBacklog bounds how many accepted stream-conns can wait for
// Accept() calls before the session accept loop applies backpressure.
const quicSessionAcceptBacklog = 128

type QuicConn struct {
	qsession *quic.Conn
	stream   *quic.Stream
	pconn    net.PacketConn // non-cached dial-side UDP socket
	listener *quic.Listener

	// streamOnly marks server-accepted conns whose Close must only close the
	// stream (the shared session/lifecycle is owned by the listener).
	streamOnly bool
	// clientSess marks dialer conns backed by the shared session cache;
	// Release, not session.Close, runs on Close.
	clientSess *quicClientSession
	// acceptCh delivers stream conns from the listener factory.
	acceptCh chan Conn
	// acceptWg tracks listener/session accept goroutines.
	acceptWg       sync.WaitGroup
	acceptChClosed sync.Once

	dialMu       sync.Mutex
	cancel       context.CancelFunc
	dialOwned    io.Closer // in-flight resource; Close() takes and closes it to abort Dial
	acceptCancel context.CancelFunc
	acceptCtx    context.Context
}

// quicClientSession is a cached client QUIC session shared by Dials to the
// same server address, along with the dial-owned UDP socket.
type quicClientSession struct {
	key       string
	session   *quic.Conn
	pconn     net.PacketConn
	mu        sync.Mutex
	refs      int
	idleTimer *time.Timer
	closed    bool
}

// release drops one reference. A session is only ever torn down at refs==0:
// normal release arms the idle timer, immediate release (dial failure/abort)
// shuts down right away. Active references always keep the session alive.
// The closed-flag transition happens inside the same critical section as the
// refcount drop so a concurrent acquire can never pick a session whose
// shutdown has already been decided.
func (cs *quicClientSession) release(immediate bool) {
	cs.mu.Lock()
	cs.refs--
	if cs.refs < 0 {
		cs.refs = 0
	}
	shutdownNow := false
	if cs.refs == 0 && !cs.closed {
		if immediate {
			cs.closed = true
			shutdownNow = true
		} else {
			cs.idleTimer = time.AfterFunc(quicSessionIdleTTL, cs.idleEvict)
		}
	}
	cs.mu.Unlock()
	if shutdownNow {
		cs.shutdown()
	}
}

func (cs *quicClientSession) idleEvict() {
	cs.mu.Lock()
	// Refs can only grow while holding cs.mu (acquire), and closed only
	// flips here or in immediate release; only the winner shuts down.
	if cs.refs != 0 || cs.closed {
		cs.mu.Unlock()
		return
	}
	cs.closed = true
	cs.mu.Unlock()
	cs.shutdown()
}

func (cs *quicClientSession) shutdown() {
	_ = cs.session.CloseWithError(0, "idle")
	if cs.pconn != nil {
		_ = cs.pconn.Close()
	}
	quicClientSessionCache.CompareAndDelete(cs.key, cs)
}

// quicClientSessionCache maps resolved server address -> shared session.
var quicClientSessionCache sync.Map

// acquireQuicSession returns a cached live session for addr or establishes a
// new one. The returned session already holds one reference for the caller.
func acquireQuicSession(ctx context.Context, addr string, control func(network, address string, c syscall.RawConn) error) (*quicClientSession, *net.UDPAddr, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, nil, err
	}
	key := udpAddr.String()

	for {
		if v, ok := quicClientSessionCache.Load(key); ok {
			cs := v.(*quicClientSession)
			cs.mu.Lock()
			valid := !cs.closed && cs.session.Context().Err() == nil
			if valid {
				if cs.idleTimer != nil {
					cs.idleTimer.Stop()
					cs.idleTimer = nil
				}
				cs.refs++
			}
			cs.mu.Unlock()
			if valid {
				return cs, udpAddr, nil
			}
			quicClientSessionCache.CompareAndDelete(key, cs)
		}

		var lc net.ListenConfig
		if control != nil {
			lc.Control = control
		}
		pconn, err := lc.ListenPacket(ctx, "udp", (&net.UDPAddr{}).String())
		if err != nil {
			return nil, nil, err
		}
		tlsConf := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"QuicConn"},
		}
		session, err := quic.Dial(ctx, pconn, udpAddr, tlsConf, nil)
		if err != nil {
			_ = pconn.Close()
			return nil, nil, err
		}
		cs := &quicClientSession{key: key, session: session, pconn: pconn, refs: 1}
		if _, loaded := quicClientSessionCache.LoadOrStore(key, cs); loaded {
			// A concurrent Dial won the race: discard our session and retry.
			_ = session.CloseWithError(0, "dedup")
			_ = pconn.Close()
			continue
		}
		return cs, udpAddr, nil
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// ownedCloser wraps an io.Closer so dialOwned identity checks use pointer
// equality (function-typed closers are not comparable and panic on ==).
type ownedCloser struct {
	c io.Closer
}

func (o *ownedCloser) Close() error {
	if o == nil || o.c == nil {
		return nil
	}
	return o.c.Close()
}

func newOwnedCloser(c io.Closer) *ownedCloser {
	return &ownedCloser{c: c}
}

func (c *QuicConn) Name() string {
	return "quic"
}

func (c *QuicConn) Read(p []byte) (n int, err error) {
	if c.stream != nil {
		return c.stream.Read(p)
	}
	return 0, errors.New("empty conn")
}

func (c *QuicConn) Write(p []byte) (n int, err error) {
	if c.stream != nil {
		return c.stream.Write(p)
	}
	return 0, errors.New("empty conn")
}

func (c *QuicConn) Close() error {
	c.dialMu.Lock()
	cancel := c.cancel
	owned := c.dialOwned
	c.cancel = nil
	c.dialOwned = nil
	// Cancel while holding dialMu so finishDial observing ctx.Err() cannot
	// race ahead of cancel and hand out a connection that Close is aborting.
	if cancel != nil {
		cancel()
	}
	c.dialMu.Unlock()
	if owned != nil {
		owned.Close()
	}

	// Listener factory: tear the listener (which closes every session) down.
	if c.acceptCancel != nil {
		c.acceptCancel()
		c.acceptCancel = nil
	}
	if c.listener != nil {
		err := c.listener.Close()
		c.acceptWg.Wait()
		// Only close the channel after all accept goroutines stopped so they
		// never send on a closed channel; unblocks Accept() with an error.
		c.acceptChClosed.Do(func() { close(c.acceptCh) })
		return err
	}

	var firstErr error
	if c.stream != nil {
		if err := c.stream.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Server-accepted and cache-backed client conns share their session:
	// closing the stream is all they own.
	if c.streamOnly {
		return firstErr
	}
	if c.clientSess != nil {
		// Normal release arms the idle timer so a quick reconnect reuses the
		// session instead of paying another handshake.
		c.clientSess.release(false)
		return firstErr
	}

	// Non-cached direct conn (legacy/defensive path): own session + socket.
	if c.qsession != nil {
		if err := c.qsession.CloseWithError(0, "close"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.pconn != nil {
		if err := c.pconn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *QuicConn) Info() string {
	if c.qsession != nil {
		return c.qsession.LocalAddr().String() + "<--quic-->" + c.qsession.RemoteAddr().String()
	}
	if c.listener != nil {
		return "quic--" + c.listener.Addr().String()
	}
	return "empty quic conn"
}

func (c *QuicConn) setDialOwned(closer io.Closer) {
	c.dialMu.Lock()
	c.dialOwned = closer
	c.dialMu.Unlock()
}

// finishDial clears dial abort state. Succeeds only if ctx is still live and
// we still own closer (Close has not stolen dialOwned).
func (c *QuicConn) finishDial(ctx context.Context, closer io.Closer) bool {
	c.dialMu.Lock()
	stillOwn := c.dialOwned == closer
	canceled := ctx.Err() != nil || !stillOwn
	c.cancel = nil
	c.dialOwned = nil
	c.dialMu.Unlock()
	if canceled {
		if closer != nil && stillOwn {
			closer.Close()
		}
		return false
	}
	return true
}

func (c *QuicConn) Dial(dst string) (Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c.dialMu.Lock()
	c.cancel = cancel
	c.dialOwned = nil
	c.dialMu.Unlock()
	defer func() {
		c.dialMu.Lock()
		owned := c.dialOwned
		c.cancel = nil
		c.dialOwned = nil
		c.dialMu.Unlock()
		if owned != nil {
			owned.Close()
		}
		cancel()
	}()

	// Get (or establish) the shared session for this server address. The
	// cache entry holds one reference until the dialed conn is delivered or
	// this Dial fails/aborts.
	cs, _, err := acquireQuicSession(ctx, dst, gControlOnConnSetup)
	if err != nil {
		return nil, err
	}
	c.setDialOwned(newOwnedCloser(closerFunc(func() error {
		// Aborted before delivery: drop the reference immediately, which
		// shuts a brand-new session down or frees a reused one.
		cs.release(true)
		return nil
	})))
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	stream, err := cs.session.OpenStreamSync(ctx)
	if err != nil {
		// Defer closes dialOwned, releasing the reference (a dead session is
		// shut down immediately and evicted so the next Dial rebuilds it).
		return nil, err
	}
	if ctx.Err() != nil {
		_ = stream.Close()
		return nil, errors.New("dial canceled")
	}

	// Hand the reference over to the returned conn: clear dial abort state so
	// the deferred closer does not release it.
	c.dialMu.Lock()
	c.dialOwned = nil
	c.cancel = nil
	c.dialMu.Unlock()
	cancel()

	return &QuicConn{qsession: cs.session, stream: stream, clientSess: cs}, nil
}

func (c *QuicConn) Listen(dst string) (Conn, error) {
	config, err := common.GenerateTLSConfig("QuicConn")
	if err != nil {
		return nil, err
	}

	listener, err := quic.ListenAddr(dst, config, nil)
	if err != nil {
		return nil, err
	}

	actx, acancel := context.WithCancel(context.Background())
	lc := &QuicConn{
		listener:     listener,
		acceptCtx:    actx,
		acceptCancel: acancel,
		acceptCh:     make(chan Conn, quicSessionAcceptBacklog),
	}
	lc.acceptWg.Add(1)
	go lc.acceptSessions(actx)
	return lc, nil
}

// acceptSessions accepts QUIC sessions and serves every stream on each
// session as an independent accepted Conn.
func (c *QuicConn) acceptSessions(ctx context.Context) {
	defer c.acceptWg.Done()
	for {
		session, err := c.listener.Accept(ctx)
		if err != nil {
			return
		}
		c.acceptWg.Add(1)
		go c.acceptStreams(ctx, session)
	}
}

func (c *QuicConn) acceptStreams(ctx context.Context, session *quic.Conn) {
	defer c.acceptWg.Done()
	defer func() { _ = session.CloseWithError(0, "session end") }()
	for {
		stream, err := session.AcceptStream(ctx)
		if err != nil {
			return
		}
		conn := &QuicConn{qsession: session, stream: stream, streamOnly: true}
		select {
		case c.acceptCh <- conn:
		case <-ctx.Done():
			_ = stream.Close()
			return
		}
	}
}

func (c *QuicConn) Accept() (Conn, error) {
	if c.listener == nil || c.acceptCh == nil {
		return nil, errors.New("not listen")
	}
	s, ok := <-c.acceptCh
	if !ok {
		return nil, errors.New("listener close")
	}
	if s == nil {
		return nil, errors.New("listener close")
	}
	return s, nil
}
