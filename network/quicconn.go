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
	"github.com/xtaci/smux"
)

/*
QuicConn 实现了基于 Quic 协议的Conn。
*/

type QuicConn struct {
	qsession *quic.Conn
	session  *smux.Session
	qsteam   *quic.Stream
	stream   *smux.Stream
	listener *quic.Listener

	dialMu    sync.Mutex
	cancel    context.CancelFunc
	dialOwned io.Closer // in-flight resource; Close() takes and closes it to abort Dial
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

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
	c.dialMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if owned != nil {
		owned.Close()
	}

	var firstErr error
	if c.stream != nil {
		if err := c.stream.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.session != nil {
		if err := c.session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.qsteam != nil {
		_ = c.qsteam.Close()
	}
	if c.qsession != nil {
		if err := c.qsession.CloseWithError(0, "close"); err != nil && firstErr == nil {
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
	if c.session != nil {
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

func quicSessionCloser(session *quic.Conn) io.Closer {
	return closerFunc(func() error {
		return session.CloseWithError(0, "dial canceled")
	})
}

// finishDial clears dial abort state. If ctx was canceled, closes closer and
// returns false so the caller must not hand closer out as a live connection.
func (c *QuicConn) finishDial(ctx context.Context, closer io.Closer) bool {
	c.dialMu.Lock()
	canceled := ctx.Err() != nil
	c.cancel = nil
	c.dialOwned = nil
	c.dialMu.Unlock()
	if canceled {
		if closer != nil {
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
	c.setDialOwned(quicSessionCloser(session))
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	ss, err := smux.Client(stream, nil)
	if err != nil {
		return nil, err
	}
	c.setDialOwned(closerFunc(func() error {
		_ = ss.Close()
		return session.CloseWithError(0, "dial canceled")
	}))
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	st, err := ss.OpenStream()
	if err != nil {
		return nil, err
	}

	final := closerFunc(func() error {
		_ = ss.Close()
		return session.CloseWithError(0, "dial canceled")
	})
	if !c.finishDial(ctx, final) {
		return nil, errors.New("dial canceled")
	}
	return &QuicConn{qsession: session, session: ss, qsteam: stream, stream: st}, nil
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

	return &QuicConn{listener: listener}, nil
}

func (c *QuicConn) Accept() (Conn, error) {
	session, err := c.listener.Accept(context.Background())
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := session.AcceptStream(ctx)
	if err != nil {
		_ = session.CloseWithError(0, "accept stream fail")
		return nil, err
	}

	ss, err := smux.Server(stream, nil)
	if err != nil {
		_ = stream.Close()
		_ = session.CloseWithError(0, "smux fail")
		return nil, err
	}

	st, err := ss.AcceptStream()
	if err != nil {
		_ = ss.Close()
		_ = session.CloseWithError(0, "accept smux stream fail")
		return nil, err
	}

	return &QuicConn{qsession: session, session: ss, qsteam: stream, stream: st}, nil
}
