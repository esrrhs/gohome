package network

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/quic-go/quic-go"
)

/*
QuicConn 实现了基于 Quic 协议的Conn。
单条 QUIC stream 直接作为读写通道（不再叠 smux）。
*/

type QuicConn struct {
	qsession *quic.Conn
	stream   *quic.Stream
	pconn    net.PacketConn // dial-side UDP socket; caller-owned, must Close explicitly
	listener *quic.Listener

	dialMu       sync.Mutex
	cancel       context.CancelFunc
	dialOwned    io.Closer // in-flight resource; Close() takes and closes it to abort Dial
	acceptCancel context.CancelFunc
	acceptCtx    context.Context
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

	if c.acceptCancel != nil {
		c.acceptCancel()
		c.acceptCancel = nil
	}

	var firstErr error
	// Do not nil stream/qsession/pconn: concurrent Read/Write/Info may still
	// observe the pointers; underlying Close is safe to call concurrently.
	if c.stream != nil {
		if err := c.stream.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.qsession != nil {
		if err := c.qsession.CloseWithError(0, "close"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// quic.Dial does not own the PacketConn (createdConn=false); close it ourselves.
	if c.pconn != nil {
		if err := c.pconn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.listener != nil {
		if err := c.listener.Close(); err != nil && firstErr == nil {
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

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"QuicConn"},
	}

	var lc net.ListenConfig
	if gControlOnConnSetup != nil {
		lc.Control = gControlOnConnSetup
	}

	laddr := &net.UDPAddr{}
	pconn, err := lc.ListenPacket(ctx, "udp", laddr.String())
	if err != nil {
		return nil, err
	}
	c.setDialOwned(pconn)
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	udpAddr, err := net.ResolveUDPAddr("udp", dst)
	if err != nil {
		return nil, err
	}

	session, err := quic.Dial(ctx, pconn, udpAddr, tlsConf, nil)
	if err != nil {
		return nil, err
	}
	// Always close both session and pconn on abort — Dial does not take pconn ownership.
	abort := newOwnedCloser(closerFunc(func() error {
		_ = session.CloseWithError(0, "dial canceled")
		return pconn.Close()
	}))
	// Swap ownership under lock immediately so Close cannot close only pconn
	// while leaving a live session.
	c.dialMu.Lock()
	c.dialOwned = abort
	canceled := ctx.Err() != nil
	c.dialMu.Unlock()
	if canceled {
		return nil, errors.New("dial canceled")
	}

	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	if !c.finishDial(ctx, abort) {
		return nil, errors.New("dial canceled")
	}
	return &QuicConn{qsession: session, stream: stream, pconn: pconn}, nil
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
	return &QuicConn{listener: listener, acceptCtx: actx, acceptCancel: acancel}, nil
}

func (c *QuicConn) Accept() (Conn, error) {
	if c.listener == nil {
		return nil, errors.New("not listen")
	}

	actx := c.acceptCtx
	if actx == nil {
		actx = context.Background()
	}

	session, err := c.listener.Accept(actx)
	if err != nil {
		return nil, err
	}

	sctx, cancel := context.WithTimeout(actx, 30*time.Second)
	defer cancel()

	stream, err := session.AcceptStream(sctx)
	if err != nil {
		_ = session.CloseWithError(0, "accept stream fail")
		return nil, err
	}

	return &QuicConn{qsession: session, stream: stream}, nil
}
