package network

import (
	"context"
	"errors"
	"net"
	"sync"
)

/*
TcpConn 实现了基于 tcp 协议的Conn。
*/

type TcpConn struct {
	conn     *net.TCPConn
	listener *net.TCPListener

	dialMu  sync.Mutex
	cancel  context.CancelFunc
	dialGen uint64
}

func (c *TcpConn) Name() string {
	return "tcp"
}

func (c *TcpConn) Read(p []byte) (n int, err error) {
	if c.conn != nil {
		return c.conn.Read(p)
	}
	return 0, errors.New("empty conn")
}

func (c *TcpConn) Write(p []byte) (n int, err error) {
	if c.conn != nil {
		return c.conn.Write(p)
	}
	return 0, errors.New("empty conn")
}

func (c *TcpConn) Close() error {
	c.dialMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	if cancel != nil {
		cancel()
	}
	c.dialMu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	} else if c.listener != nil {
		return c.listener.Close()
	}
	return nil
}

func (c *TcpConn) Info() string {
	// Compute each call so concurrent readers never race on a lazy cache.
	if c.conn != nil {
		return c.conn.LocalAddr().String() + "<--tcp-->" + c.conn.RemoteAddr().String()
	}
	if c.listener != nil {
		return "tcp--" + c.listener.Addr().String()
	}
	return "empty tcp conn"
}

func (c *TcpConn) Dial(dst string) (Conn, error) {
	addr, err := net.ResolveTCPAddr("tcp", dst)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.dialMu.Lock()
	c.dialGen++
	gen := c.dialGen
	c.cancel = cancel
	c.dialMu.Unlock()
	defer func() {
		c.dialMu.Lock()
		if c.dialGen == gen {
			c.cancel = nil
		}
		c.dialMu.Unlock()
		cancel()
	}()

	var d net.Dialer
	if gControlOnConnSetup != nil {
		d = net.Dialer{Control: gControlOnConnSetup}
	}
	conn, err := d.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, errors.New("dial canceled")
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("tcp dial: unexpected conn type")
	}
	return &TcpConn{conn: tcpConn}, nil
}

func (c *TcpConn) Listen(dst string) (Conn, error) {
	addr, err := net.ResolveTCPAddr("tcp", dst)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &TcpConn{listener: listener}, nil
}

func (c *TcpConn) Accept() (Conn, error) {
	if c.listener == nil {
		return nil, errors.New("not listen")
	}
	conn, err := c.listener.Accept()
	if err != nil {
		return nil, err
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("tcp accept: unexpected conn type")
	}
	return &TcpConn{conn: tcpConn}, nil
}
