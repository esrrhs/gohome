package network

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/xtaci/kcp-go"
)

/*
KcpConn 实现了基于 KCP 协议的Conn。
UDPSession 已是 stream 模式 net.Conn，直接读写（不再叠 smux）。
*/

type KcpConn struct {
	sess     *kcp.UDPSession
	listener *kcp.Listener

	dialMu    sync.Mutex
	cancel    context.CancelFunc
	dialOwned io.Closer // in-flight resource; Close() takes and closes it to abort Dial
}

func (c *KcpConn) Name() string {
	return "kcp"
}

func (c *KcpConn) Read(p []byte) (n int, err error) {
	if c.sess != nil {
		return c.sess.Read(p)
	}
	return 0, errors.New("empty conn")
}

func (c *KcpConn) Write(p []byte) (n int, err error) {
	if c.sess != nil {
		return c.sess.Write(p)
	}
	return 0, errors.New("empty conn")
}

func (c *KcpConn) Close() error {
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

	if c.sess != nil {
		return c.sess.Close()
	} else if c.listener != nil {
		return c.listener.Close()
	}
	return nil
}

func (c *KcpConn) Info() string {
	if c.sess != nil {
		return c.sess.LocalAddr().String() + "<--kcp-->" + c.sess.RemoteAddr().String()
	}
	if c.listener != nil {
		return "kcp--" + c.listener.Addr().String()
	}
	return "empty kcp conn"
}

func (c *KcpConn) setDialOwned(closer io.Closer) {
	c.dialMu.Lock()
	c.dialOwned = closer
	c.dialMu.Unlock()
}

// finishDial clears dial abort state. If ctx was canceled, closes closer and
// returns false so the caller must not hand closer out as a live connection.
func (c *KcpConn) finishDial(ctx context.Context, closer io.Closer) bool {
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

func (c *KcpConn) Dial(dst string) (Conn, error) {
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

	conn, err := kcp.NewConn(dst, nil, 0, 0, pconn.(*net.UDPConn))
	if err != nil {
		return nil, err
	}
	// kcp owns pconn; abort via closing the kcp session.
	c.setDialOwned(conn)
	if ctx.Err() != nil {
		return nil, errors.New("dial canceled")
	}

	c.setParam(conn)

	if !c.finishDial(ctx, conn) {
		return nil, errors.New("dial canceled")
	}
	return &KcpConn{sess: conn}, nil
}

func (c *KcpConn) Listen(dst string) (Conn, error) {
	listener, err := kcp.Listen(dst)
	if err != nil {
		return nil, err
	}

	listener.(*kcp.Listener).SetReadBuffer(4 * 1024 * 1024)
	listener.(*kcp.Listener).SetWriteBuffer(4 * 1024 * 1024)
	listener.(*kcp.Listener).SetDSCP(46)

	return &KcpConn{listener: listener.(*kcp.Listener)}, nil
}

func (c *KcpConn) Accept() (Conn, error) {
	conn, err := c.listener.Accept()
	if err != nil {
		return nil, err
	}

	sess := conn.(*kcp.UDPSession)
	c.setParam(sess)
	return &KcpConn{sess: sess}, nil
}

func (c *KcpConn) setParam(conn *kcp.UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(10000, 10000)
	conn.SetReadBuffer(16 * 1024 * 1024)
	conn.SetWriteBuffer(16 * 1024 * 1024)
	// nodelay on, 20ms update, fast resend, non-congestion-control nc=1
	conn.SetNoDelay(1, 20, 2, 1)
	conn.SetMtu(1200)
	conn.SetACKNoDelay(true)
}
