package network

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/thread"
	"golang.org/x/net/ipv4"
	"google.golang.org/protobuf/proto"
)

/*
RudpConn 实现了基于 可靠udp 协议的Conn。
*/

type RudpConfig struct {
	MaxPacketSize      int
	CutSize            int
	MaxId              int
	BufferSize         int
	MaxWin             int
	ResendTimems       int
	Compress           int
	Stat               int
	ConnectTimeoutMs   int
	CloseTimeoutMs     int
	CloseWaitTimeoutMs int
	AcceptChanLen      int
	Congestion         string
	BatchSendPkgs      int
}

func DefaultRudpConfig() *RudpConfig {
	return &RudpConfig{
		MaxPacketSize:      2048,
		CutSize:            1200,
		MaxId:              100000,
		BufferSize:         1024 * 1024,
		MaxWin:             10000,
		ResendTimems:       200,
		Compress:           0,
		Stat:               0,
		ConnectTimeoutMs:   10000,
		CloseTimeoutMs:     5000,
		CloseWaitTimeoutMs: 5000,
		AcceptChanLen:      128,
		Congestion:         "bb",
		BatchSendPkgs:      64,
	}
}

type RudpConn struct {
	config        *RudpConfig
	dialer        *rudpConnDialer
	listenersonny *rudpConnListenerSonny
	listener      *rudpConnListener
	dialMu        sync.Mutex
	cancel        context.CancelFunc
	dialGen       uint64
	isclose       atomic.Bool
	closelock     sync.Mutex
	cfgMu         sync.RWMutex
}

type rudpConnDialer struct {
	conn *net.UDPConn
	fm   *FrameMgr
	wg   *thread.Group
}

type rudpConnListenerSonny struct {
	dstaddr    *net.UDPAddr
	fatherconn *net.UDPConn
	listener   *rudpConnListener
	fm         *FrameMgr
	wg         *thread.Group
}

type rudpConnListener struct {
	listenerconn *net.UDPConn
	wg           *thread.Group
	sonny        sync.Map
	accept       *common.Channel
}

func (c *RudpConn) Name() string {
	return "rudp"
}

func (c *RudpConn) Read(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("read closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("read empty buffer")
	}

	var fm *FrameMgr
	var wg *thread.Group
	if c.dialer != nil {
		fm = c.dialer.fm
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be read")
	} else if c.listenersonny != nil {
		fm = c.listenersonny.fm
		wg = c.listenersonny.wg
	} else {
		return 0, errors.New("empty conn")
	}

	for !c.isclose.Load() {
		if fm.GetRecvBufferSize() <= 0 {
			if wg != nil && wg.IsExit() {
				return 0, errors.New("closed conn")
			}
			time.Sleep(time.Millisecond * 100)
			continue
		}

		size := copy(p, fm.GetRecvReadLineBuffer())
		fm.SkipRecvBuffer(size)
		return size, nil
	}

	return 0, errors.New("read closed conn")
}

func (c *RudpConn) Write(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("write closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("write empty data")
	}

	var fm *FrameMgr
	var wg *thread.Group
	if c.dialer != nil {
		fm = c.dialer.fm
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be write")
	} else if c.listenersonny != nil {
		fm = c.listenersonny.fm
		wg = c.listenersonny.wg
	} else {
		return 0, errors.New("empty conn")
	}

	totalsize := len(p)
	cur := 0

	for !c.isclose.Load() {
		size := totalsize - cur
		svleft := fm.GetSendBufferLeft()
		if size > svleft {
			size = svleft
		}

		if size <= 0 {
			if wg != nil && wg.IsExit() {
				if cur > 0 {
					return cur, errors.New("closed conn")
				}
				return 0, errors.New("closed conn")
			}
			time.Sleep(time.Millisecond * 100)
			continue
		}

		fm.WriteSendBuffer(p[cur : cur+size])
		cur += size

		if cur >= totalsize {
			return totalsize, nil
		}

		time.Sleep(time.Millisecond * 100)
	}

	if cur > 0 {
		return cur, errors.New("write closed conn")
	}
	return 0, errors.New("write closed conn")
}

func (c *RudpConn) Close() error {
	c.checkConfig()

	if c.isclose.Load() {
		return nil
	}

	c.closelock.Lock()
	defer c.closelock.Unlock()

	if c.isclose.Load() {
		return nil
	}

	// Mark closed first so Read/Write observe it before we tear down the socket.
	c.isclose.Store(true)

	c.dialMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	if cancel != nil {
		cancel()
	}
	c.dialMu.Unlock()
	if c.dialer != nil {
		if c.dialer.wg != nil {
			c.dialer.wg.Stop()
			// Join (not Wait): Wait returns as soon as Stop closes donech.
			_ = c.dialer.wg.Join()
		}
		if c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	} else if c.listener != nil {
		if c.listener.accept != nil {
			c.listener.accept.Close()
		}
		if c.listener.listenerconn != nil {
			c.listener.listenerconn.Close()
		}
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RudpConn)
			_ = u.Close()
			return true
		})
		if c.listener.wg != nil {
			c.listener.wg.Stop()
			_ = c.listener.wg.Join()
		}
	} else if c.listenersonny != nil {
		if c.listenersonny.wg != nil {
			c.listenersonny.wg.Stop()
			_ = c.listenersonny.wg.Join()
		}
		if c.listenersonny.listener != nil && c.listenersonny.dstaddr != nil {
			c.listenersonny.listener.sonny.Delete(c.listenersonny.dstaddr.String())
		}
	}

	return nil
}

func (c *RudpConn) Info() string {
	c.checkConfig()

	if c.dialer != nil {
		return c.dialer.conn.LocalAddr().String() + "<--rudp-->" + c.dialer.conn.RemoteAddr().String()
	}
	if c.listener != nil {
		return "rudp--" + c.listener.listenerconn.LocalAddr().String()
	}
	if c.listenersonny != nil {
		return c.listenersonny.fatherconn.LocalAddr().String() + "<--rudp-->" + c.listenersonny.dstaddr.String()
	}
	return "empty rudp conn"
}

func (c *RudpConn) Dial(dst string) (Conn, error) {
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

	id := common.Guid()
	fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
	fm.SetDebugid(id)
	if c.config.Congestion == "bb" {
		fm.SetCongestion(&BBCongestion{})
	}

	dialer := &rudpConnDialer{conn: conn.(*net.UDPConn), fm: fm}

	u := &RudpConn{config: c.config, dialer: dialer}

	//loggo.Debug("start connect remote rudp %s %s", u.Info(), id)

	u.dialer.fm.Connect()

	startConnectTime := time.Now()
	buf := make([]byte, c.config.MaxPacketSize)
	for {
		if u.dialer.fm.IsConnected() {
			break
		}

		u.dialer.fm.Update()

		// send udp
		sendlist := u.dialer.fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, _ := u.dialer.fm.MarshalFrame(f)
			u.dialer.conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			u.dialer.conn.Write(mb)
		}

		// recv udp
		u.dialer.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		n, _ := u.dialer.conn.Read(buf)
		if n > 0 {
			f := &Frame{}
			err := proto.Unmarshal(buf[0:n], f)
			if err == nil {
				u.dialer.fm.OnRecvFrame(f)
			} else {
				//loggo.Error("%s %s Unmarshal fail %s", c.Info(), u.Info(), err)
				break
			}
		}

		if c.isclose.Load() {
			//loggo.Debug("can not connect remote rudp %s", u.Info())
			break
		}

		// timeout
		now := time.Now()
		diffclose := now.Sub(startConnectTime)
		if diffclose > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			//loggo.Debug("can not connect remote rudp %s", u.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	if c.isclose.Load() {
		u.Close()
		return nil, errors.New("closed conn")
	}

	if u.isclose.Load() {
		return nil, errors.New("closed conn")
	}

	if !u.dialer.fm.IsConnected() {
		u.Close()
		return nil, errors.New("connect timeout")
	}

	//loggo.Debug("connect remote ok rudp %s", u.Info())

	wg := thread.NewGroup("RudpConn Dialer"+" "+u.Info(), nil, nil)

	u.dialer.wg = wg

	wg.Go("RudpConn updateDialerSonny"+" "+u.Info(), func() error {
		return u.updateDialerSonny()
	})

	return u, nil
}

func (c *RudpConn) Listen(dst string) (Conn, error) {
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

	wg := thread.NewGroup("RudpConn Listen"+" "+dst, nil, nil)

	listener := &rudpConnListener{
		listenerconn: listenerconn,
		wg:           wg,
		accept:       ch,
	}

	u := &RudpConn{config: c.config, listener: listener}
	wg.Go("RudpConn loopListenerRecv"+" "+dst, func() error {
		return u.loopListenerRecv()
	})

	return u, nil
}

func (c *RudpConn) Accept() (Conn, error) {
	c.checkConfig()

	if c.listener == nil || c.listener.wg == nil {
		return nil, errors.New("not listen")
	}
	for !c.listener.wg.IsExit() {
		s := <-c.listener.accept.Ch()
		if s == nil {
			break
		}
		sonny := s.(*RudpConn)
		_, ok := c.listener.sonny.Load(sonny.listenersonny.dstaddr.String())
		if !ok {
			continue
		}
		if sonny.isclose.Load() {
			continue
		}
		return sonny, nil
	}
	return nil, errors.New("listener close")
}

func (c *RudpConn) checkConfig() {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRudpConfig()
	}
	c.cfgMu.Unlock()
}

func (c *RudpConn) SetConfig(config *RudpConfig) {
	c.cfgMu.Lock()
	c.config = config
	c.cfgMu.Unlock()
}

func (c *RudpConn) GetConfig() *RudpConfig {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRudpConfig()
	}
	cfg := c.config
	c.cfgMu.Unlock()
	return cfg
}

func (c *RudpConn) loopListenerRecv() error {
	c.checkConfig()

	buf := make([]byte, c.config.MaxPacketSize)
	for !c.listener.wg.IsExit() {
		c.listener.listenerconn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		n, srcaddr, err := c.listener.listenerconn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		srcaddrstr := srcaddr.String()

		v, ok := c.listener.sonny.Load(srcaddrstr)
		if !ok {
			id := common.Guid()
			fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
			fm.SetDebugid(id)
			if c.config.Congestion == "bb" {
				fm.SetCongestion(&BBCongestion{})
			}

			sonny := &rudpConnListenerSonny{
				dstaddr:    srcaddr,
				fatherconn: c.listener.listenerconn,
				listener:   c.listener,
				fm:         fm,
			}

			u := &RudpConn{config: c.config, listenersonny: sonny}
			c.listener.sonny.Store(srcaddrstr, u)

			// Feed the first packet (often CONNECT) into FrameMgr immediately;
			// previously it was dropped and relied solely on retransmission.
			f := &Frame{}
			if err := proto.Unmarshal(buf[0:n], f); err == nil {
				u.listenersonny.fm.OnRecvFrame(f)
			}

			c.listener.wg.Go("RudpConn accept"+" "+u.Info(), func() error {
				return c.accept(u)
			})

			//loggo.Debug("start accept remote rudp %s %s", u.Info(), id)
		} else {
			u := v.(*RudpConn)
			if u.isclose.Load() {
				c.listener.sonny.Delete(srcaddrstr)
				continue
			}

			f := &Frame{}
			err := proto.Unmarshal(buf[0:n], f)
			if err == nil {
				u.listenersonny.fm.OnRecvFrame(f)
				//loggo.Debug("%s recv frame %d", u.Info(), f.Id)
			} else {
				//loggo.Error("%s %s Unmarshal fail %s", c.Info(), u.Info(), err)
			}
		}

		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RudpConn)
			if u.isclose.Load() {
				c.listener.sonny.Delete(key)
				//loggo.Debug("delete sonny from map %s", u.Info())
			}
			return true
		})
	}
	return nil
}

func (c *RudpConn) accept(u *RudpConn) error {

	//loggo.Debug("server begin accept rudp %s", u.Info())

	startConnectTime := time.Now()
	done := false
	for !c.listener.wg.IsExit() {

		if u.listenersonny.fm.IsConnected() {
			done = true
			break
		}

		u.listenersonny.fm.Update()

		// send udp
		sendlist := u.listenersonny.fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := u.listenersonny.fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				break
			}
			u.listenersonny.fatherconn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			u.listenersonny.fatherconn.WriteToUDP(mb, u.listenersonny.dstaddr)
		}

		now := time.Now()
		diffclose := now.Sub(startConnectTime)
		if diffclose > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			//loggo.Debug("can not connect by remote rudp %s", u.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	if !done {
		u.Close()
		return nil
	}

	if c.listener.wg.IsExit() {
		u.Close()
		return nil
	}

	//loggo.Debug("server accept rudp ok %s", u.Info())

	wg := thread.NewGroup("RudpConn ListenerSonny"+" "+u.Info(), c.listener.wg, nil)
	u.listenersonny.wg = wg

	wg.Go("RudpConn updateListenerSonny"+" "+u.Info(), func() error {
		return u.updateListenerSonny()
	})

	// Non-blocking accept enqueue: blocking holds the accept goroutine under backlog.
	if !c.listener.accept.WriteTimeout(u, 100) {
		u.Close()
		return nil
	}

	//loggo.Debug("accept rudp finish %s", u.Info())

	return nil
}

func (c *RudpConn) updateListenerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.listenersonny != nil && c.listenersonny.listener != nil && c.listenersonny.dstaddr != nil {
			c.listenersonny.listener.sonny.Delete(c.listenersonny.dstaddr.String())
		}
	}()
	return c.update_rudp(c.listenersonny.wg, c.listenersonny.fm, c.listenersonny.fatherconn, c.listenersonny.dstaddr, false)
}

func (c *RudpConn) updateDialerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.dialer != nil && c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	}()
	return c.update_rudp(c.dialer.wg, c.dialer.fm, c.dialer.conn, nil, true)
}

func (c *RudpConn) update_rudp(wg *thread.Group, fm *FrameMgr, conn *net.UDPConn, dstaddr *net.UDPAddr, readconn bool) error {

	//loggo.Debug("start rudp conn %s", c.Info())

	const (
		stageOpen      int32 = 0
		stageClose     int32 = 1
		stageCloseWait int32 = 2
	)
	var stage atomic.Int32
	stage.Store(stageOpen)

	if readconn {
		wg.Go("RudpConn update_rudp recv"+" "+c.Info(), func() error {
			bytes := make([]byte, c.config.MaxPacketSize)
			for !wg.IsExit() && stage.Load() != stageCloseWait {
				// recv udp
				conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
				n, _ := conn.Read(bytes)
				if n > 0 {
					f := &Frame{}
					err := proto.Unmarshal(bytes[0:n], f)
					if err == nil {
						fm.OnRecvFrame(f)
						//loggo.Debug("%s recv frame %d", c.Info(), f.Id)
					} else {
						//loggo.Error("Unmarshal fail from %s %s", c.Info(), err)
					}
				}
			}

			return nil
		})
	}

	reason := ""

	pconn := ipv4.NewPacketConn(conn)
	// 预分配消息数组，避免循环内分配
	msgs := make([]ipv4.Message, 0, c.config.BatchSendPkgs)
	count := 0

	for !wg.IsExit() {

		avctive := fm.Update()

		// send udp
		sendlist := fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				reason = "MarshalFrame"
				break
			}

			if runtime.GOOS != "linux" {
				conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
				if dstaddr != nil {
					conn.WriteToUDP(mb, dstaddr)
					//loggo.Debug("%s send frame to %s %d", c.Info(), dstaddr, f.Id)
				} else {
					conn.Write(mb)
					//loggo.Debug("%s send frame %d", c.Info(), f.Id)
				}
			} else {
				// 构造批量消息
				msg := ipv4.Message{
					Buffers: [][]byte{mb}, // 这里直接引用 mb，没有拷贝
				}
				if dstaddr != nil {
					msg.Addr = dstaddr
				}
				msgs = append(msgs, msg)
				count++

				// 如果积攒够了一批，或者列表到头了，就发送
				if count >= c.config.BatchSendPkgs || e.Next() == nil {
					conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))

					// WriteBatch 会调用底层的 sendmmsg
					_, err := pconn.WriteBatch(msgs, 0)
					if err != nil {
						reason = "WriteBatch"
						msgs = msgs[:0]
						count = 0
						break
					}

					// 重置 slice 长度以便复用 (保留容量)
					msgs = msgs[:0]
					count = 0
				}
			}
		}

		if reason == "MarshalFrame" || reason == "WriteBatch" {
			break
		}

		// timeout
		if fm.IsHBTimeout() {
			reason = "HBTimeout"
			//loggo.Debug("close inactive conn %s", c.Info())
			break
		}

		if fm.IsRemoteClosed() {
			reason = "RemoteClose"
			//loggo.Debug("closed by remote conn %s", c.Info())
			break
		}

		if !avctive && sendlist.Len() <= 0 {
			time.Sleep(time.Millisecond * 10)
		}
	}

	stage.Store(stageClose)
	fm.Close()
	//loggo.Debug("close rudp conn fm %s", c.Info())

	startCloseTime := time.Now()
	for !wg.IsExit() {
		now := time.Now()

		fm.Update()

		// send udp
		sendlist := fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				break
			}
			conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			if dstaddr != nil {
				conn.WriteToUDP(mb, dstaddr)
				//loggo.Debug("%s send frame to %s %d", c.Info(), dstaddr, f.Id)
			} else {
				conn.Write(mb)
				//loggo.Debug("%s send frame %d", c.Info(), f.Id)
			}
		}

		diffclose := now.Sub(startCloseTime)
		if diffclose > time.Millisecond*time.Duration(c.config.CloseTimeoutMs) {
			//loggo.Debug("close conn had timeout %s", c.Info())
			break
		}

		remoteclosed := fm.IsRemoteClosed()
		if remoteclosed {
			//loggo.Debug("remote conn had closed %s", c.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	stage.Store(stageCloseWait)
	//loggo.Debug("close rudp conn update %s", c.Info())

	startEndTime := time.Now()
	for !wg.IsExit() {
		now := time.Now()

		diffclose := now.Sub(startEndTime)
		if diffclose > time.Millisecond*time.Duration(c.config.CloseWaitTimeoutMs) {
			//loggo.Debug("close wait conn had timeout %s", c.Info())
			break
		}

		if fm.GetRecvBufferSize() <= 0 {
			//loggo.Debug("conn had no data %s", c.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	//loggo.Debug("close rudp conn %s", c.Info())

	return errors.New("closed " + reason)
}
