package network

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	"github.com/esrrhs/gohome/thread"
)

/*
UdpConn 实现了基于 udp 协议的Conn。
*/

type UdpConn struct {
	config        *UdpConfig
	dialer        *udpConnDialer
	listenersonny *udpConnListenerSonny
	listener      *udpConnListener

	dialMu sync.Mutex
	cancel context.CancelFunc
	dialGen uint64
	cfgMu sync.RWMutex
}

type udpConnDialer struct {
	conn *net.UDPConn
}

type udpConnListenerSonny struct {
	dstaddr    *net.UDPAddr
	fatherconn *net.UDPConn
	listener   *udpConnListener
	recvch     *common.Channel
	isclose    int32
	closeOnce  sync.Once
}

type udpConnListener struct {
	listenerconn *net.UDPConn
	wg           *thread.Group
	sonny        sync.Map
	accept       *common.Channel
}

type UdpConfig struct {
	MaxPacketSize       int
	RecvChanLen         int
	AcceptChanLen       int
	RecvChanPushTimeout int
}

func DefaultUdpConfig() *UdpConfig {
	return &UdpConfig{
		MaxPacketSize:       10240,
		RecvChanLen:         128,
		AcceptChanLen:       128,
		RecvChanPushTimeout: 100,
	}
}

func (c *UdpConn) Name() string {
	return "udp"
}

func (c *UdpConn) Read(p []byte) (n int, err error) {
	c.checkConfig()

	if c.dialer != nil {
		return c.dialer.conn.Read(p)
	} else if c.listener != nil {
		return 0, errors.New("listener can not be read")
	} else if c.listenersonny != nil {
		if atomic.LoadInt32(&c.listenersonny.isclose) != 0 {
			return 0, errors.New("read closed conn")
		}
		b := <-c.listenersonny.recvch.Ch()
		if b == nil {
			return 0, errors.New("read closed conn")
		}
		data := b.([]byte)
		if len(data) > len(p) {
			return 0, errors.New("read buffer too small")
		}
		copy(p, data)
		return len(data), nil
	}
	return 0, errors.New("empty conn")
}

func (c *UdpConn) Write(p []byte) (n int, err error) {
	c.checkConfig()

	if c.dialer != nil {
		return c.dialer.conn.Write(p)
	} else if c.listener != nil {
		return 0, errors.New("listener can not be write")
	} else if c.listenersonny != nil {
		if atomic.LoadInt32(&c.listenersonny.isclose) != 0 {
			return 0, errors.New("write closed conn")
		}
		return c.listenersonny.fatherconn.WriteToUDP(p, c.listenersonny.dstaddr)
	}
	return 0, errors.New("empty conn")
}

func (c *UdpConn) Close() error {
	c.checkConfig()

	c.dialMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	if cancel != nil {
		cancel()
	}
	c.dialMu.Unlock()

	if c.dialer != nil {
		return c.dialer.conn.Close()
	} else if c.listener != nil {
		if c.listener.wg != nil {
			c.listener.wg.Stop()
			// Join (not Wait): Wait returns as soon as Stop closes donech.
			_ = c.listener.wg.Join()
		}
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*UdpConn)
			_ = u.Close()
			return true
		})
	} else if c.listenersonny != nil {
		c.listenersonny.closeOnce.Do(func() {
			// Mark closed before closing the recv channel so Write stops first.
			atomic.StoreInt32(&c.listenersonny.isclose, 1)
			c.listenersonny.recvch.Close()
			if c.listenersonny.listener != nil && c.listenersonny.dstaddr != nil {
				c.listenersonny.listener.sonny.Delete(c.listenersonny.dstaddr.String())
			}
		})
	}
	return nil
}

func (c *UdpConn) Info() string {
	c.checkConfig()

	if c.dialer != nil {
		return c.dialer.conn.LocalAddr().String() + "<--udp-->" + c.dialer.conn.RemoteAddr().String()
	}
	if c.listener != nil {
		return "udp--" + c.listener.listenerconn.LocalAddr().String()
	}
	if c.listenersonny != nil {
		return c.listenersonny.fatherconn.LocalAddr().String() + "<--udp-->" + c.listenersonny.dstaddr.String()
	}
	return "empty udp conn"
}

func (c *UdpConn) Dial(dst string) (Conn, error) {
	c.checkConfig()

	addr, err := net.ResolveUDPAddr("udp", dst)
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
	conn, err := d.DialContext(ctx, "udp", addr.String())
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, errors.New("dial canceled")
	}

	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("udp dial: unexpected conn type")
	}
	dialer := &udpConnDialer{conn: udpConn}
	return &UdpConn{config: c.config, dialer: dialer}, nil
}

func (c *UdpConn) Listen(dst string) (Conn, error) {
	c.checkConfig()

	ipaddr, err := net.ResolveUDPAddr("udp", dst)
	if err != nil {
		return nil, err
	}

	listenerconn, err := net.ListenUDP("udp", ipaddr)
	if err != nil {
		return nil, err
	}

	ch := common.NewChannel(c.config.AcceptChanLen)

	wg := thread.NewGroup("UdpConn Listen"+" "+dst, nil, func() {
		listenerconn.Close()
		ch.Close()
	})

	listener := &udpConnListener{
		listenerconn: listenerconn,
		wg:           wg,
		accept:       ch,
	}

	u := &UdpConn{config: c.config, listener: listener}
	wg.Go("UdpConn Listen loopRecv"+" "+dst, func() error {
		return u.loopRecv()
	})

	return u, nil
}

func (c *UdpConn) Accept() (Conn, error) {
	c.checkConfig()

	if c.listener == nil || c.listener.wg == nil {
		return nil, errors.New("not listen")
	}
	for !c.listener.wg.IsExit() {
		s := <-c.listener.accept.Ch()
		if s == nil {
			break
		}
		sonny := s.(*UdpConn)
		_, ok := c.listener.sonny.Load(sonny.listenersonny.dstaddr.String())
		if !ok {
			continue
		}
		if atomic.LoadInt32(&sonny.listenersonny.isclose) != 0 {
			continue
		}
		return sonny, nil
	}
	return nil, errors.New("listener close")
}

func (c *UdpConn) loopRecv() error {
	c.checkConfig()

	pushTimeout := c.config.RecvChanPushTimeout
	if pushTimeout <= 0 {
		pushTimeout = 100
	}

	buf := make([]byte, c.config.MaxPacketSize)
	for !c.listener.wg.IsExit() {
		n, srcaddr, err := c.listener.listenerconn.ReadFromUDP(buf)
		if err != nil {
			if c.listener.wg.IsExit() {
				return nil
			}
			return err
		}

		data := make([]byte, n)
		copy(data, buf[0:n])
		srcaddrstr := srcaddr.String()

		v, ok := c.listener.sonny.Load(srcaddrstr)
		if !ok {
			sonny := &udpConnListenerSonny{
				dstaddr:    srcaddr,
				fatherconn: c.listener.listenerconn,
				listener:   c.listener,
				recvch:     common.NewChannel(c.config.RecvChanLen),
			}

			u := &UdpConn{config: c.config, listenersonny: sonny}
			if !u.listenersonny.recvch.WriteTimeout(data, pushTimeout) {
				loggo.Debug("udp conn %s push %d data to %s recv channel timeout", c.Info(), len(data), u.Info())
			}
			c.listener.sonny.Store(srcaddrstr, u)

			// Non-blocking accept enqueue: blocking here stalls the whole recv loop.
			if !c.listener.accept.WriteTimeout(u, pushTimeout) {
				_ = u.Close()
				continue
			}
		} else {
			u := v.(*UdpConn)
			if atomic.LoadInt32(&u.listenersonny.isclose) != 0 {
				c.listener.sonny.Delete(srcaddrstr)
				continue
			}
			if !u.listenersonny.recvch.WriteTimeout(data, pushTimeout) {
				loggo.Debug("udp conn %s push %d data to %s recv channel timeout", c.Info(), len(data), u.Info())
			}
		}

		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*UdpConn)
			if atomic.LoadInt32(&u.listenersonny.isclose) != 0 {
				c.listener.sonny.Delete(key)
			}
			return true
		})
	}
	return nil
}

func (c *UdpConn) checkConfig() {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultUdpConfig()
	}
	c.cfgMu.Unlock()
}

func (c *UdpConn) SetConfig(config *UdpConfig) {
	c.cfgMu.Lock()
	c.config = config
	c.cfgMu.Unlock()
}

func (c *UdpConn) GetConfig() *UdpConfig {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultUdpConfig()
	}
	cfg := c.config
	c.cfgMu.Unlock()
	return cfg
}
