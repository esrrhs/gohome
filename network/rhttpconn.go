package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/list"
	"github.com/esrrhs/gohome/thread"
)

/*
RhttpConn 实现了基于 可靠http 协议的Conn。
*/

type HttpConfig struct {
	MaxPacketSize       int
	RecvChanLen         int
	AcceptChanLen       int
	RecvChanPushTimeout int
	BufferSize          int
	MaxRetryNum         int
	CloseWaitTimeoutMs  int
	HBTimeoutMs         int
	MaxMsgIndex         int
	RequestTimeoutMs    int // per-request HTTP timeout; 0 means default
}

func DefaultHttpConfig() *HttpConfig {
	return &HttpConfig{
		MaxPacketSize:       1024 * 100,
		RecvChanLen:         128,
		AcceptChanLen:       128,
		RecvChanPushTimeout: 100,
		BufferSize:          1024 * 1024,
		MaxRetryNum:         10,
		CloseWaitTimeoutMs:  5000,
		HBTimeoutMs:         10000,
		MaxMsgIndex:         100,
		RequestTimeoutMs:    15000,
	}
}

const (
	ProtoConnnect = "connect"
	ProtoData     = "data"
	ProtoClose    = "close"

	ProtoCodeOK   = 200
	ProtoCodeFull = 403
	ProtoCodeFail = 404
)

type RhttpConn struct {
	id            string
	isclose       atomic.Bool
	config        *HttpConfig
	dialer        *httpConnDialer
	listenersonny *httpConnListenerSonny
	listener      *httpConnListener
	cancel        context.CancelFunc
	sendb         *list.RBuffergo
	recvb         *list.RBuffergo
	closelock     sync.Mutex
}

type httpConnDialer struct {
	wg    *thread.Group
	addr  string
	url   string
	index int
	retry int
	ctx   context.Context
	tp    *http.Transport // reused for all posts; closed on Dialer Close
}

type httpConnListenerSonny struct {
	fwg          *thread.Group
	listener     *httpConnListener
	addr         string
	expectIndex  int
	lastRecvTime time.Time
	lastSend     []byte
	mu           sync.Mutex
}

type httpConnListener struct {
	wg           *thread.Group
	addr         string
	listenerconn *net.TCPListener
	srv          *http.Server
	sonny        sync.Map
	accept       *common.Channel
}

func (c *RhttpConn) Name() string {
	return "rhttp"
}

func (c *RhttpConn) Read(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("read closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("read empty buffer")
	}

	var wg *thread.Group
	if c.dialer != nil {
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be read")
	} else if c.listenersonny != nil {
		wg = c.listenersonny.fwg
	} else {
		return 0, errors.New("empty conn")
	}

	for !c.isclose.Load() {
		size := c.recvb.Size()
		if size <= 0 {
			if wg != nil && wg.IsExit() {
				return 0, errors.New("closed conn")
			}
			time.Sleep(time.Millisecond * 100)
			continue
		}

		if size > len(p) {
			size = len(p)
		}
		if !c.recvb.Read(p[0:size]) {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		return size, nil
	}

	return 0, errors.New("read closed conn")
}

func (c *RhttpConn) Write(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("write closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("write empty data")
	}

	var wg *thread.Group
	if c.dialer != nil {
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be write")
	} else if c.listenersonny != nil {
		wg = c.listenersonny.fwg
	} else {
		return 0, errors.New("empty conn")
	}

	totalsize := len(p)
	cur := 0

	for !c.isclose.Load() {
		size := totalsize - cur
		svleft := c.sendb.Capacity() - c.sendb.Size()
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

		c.sendb.Write(p[cur : cur+size])
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

func (c *RhttpConn) Close() error {
	c.checkConfig()

	if c.isclose.Load() {
		return nil
	}

	c.closelock.Lock()
	defer c.closelock.Unlock()

	if c.isclose.Load() {
		return nil
	}

	// Mark closed first so Read/Write/updateDialerSonny observe it.
	c.isclose.Store(true)

	if c.dialer != nil {
		if c.cancel != nil {
			c.cancel()
			c.cancel = nil
		}
		if c.dialer.wg != nil {
			c.dialer.wg.Stop()
			// Join (not Wait): Wait returns as soon as Stop closes donech and
			// would race with updateDialerSonny still using the Transport.
			_ = c.dialer.wg.Join()
		}
		// ProtoClose after the data loop has stopped so the server never sees
		// Close concurrent with an in-flight Data POST for the same id.
		if c.dialer.url != "" {
			_, _, _ = c.postData(context.Background(), c.dialer.url+"?type="+ProtoClose, []byte{})
		}
		if c.dialer.tp != nil {
			c.dialer.tp.CloseIdleConnections()
			c.dialer.tp = nil
		}
	} else if c.listener != nil {
		if c.cancel != nil {
			c.cancel()
			c.cancel = nil
		}
		if c.listener.srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = c.listener.srv.Shutdown(ctx)
			cancel()
		}
		if c.listener.listenerconn != nil {
			c.listener.listenerconn.Close()
		}
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RhttpConn)
			_ = u.Close()
			return true
		})
		if c.listener.wg != nil {
			c.listener.wg.Stop()
			_ = c.listener.wg.Join()
		}
	} else if c.listenersonny != nil {
		if c.cancel != nil {
			c.cancel()
			c.cancel = nil
		}
		if c.listenersonny.listener != nil {
			c.listenersonny.listener.sonny.Delete(c.id)
		}
	} else if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	return nil
}

func (c *RhttpConn) Info() string {
	c.checkConfig()

	if c.dialer != nil {
		return c.id + "<--rhttp dialer-->" + c.dialer.addr
	}
	if c.listener != nil {
		return "rhttp listener--" + c.listener.addr
	}
	if c.listenersonny != nil {
		return c.id + "<--rhttp listenersonny-->" + c.listenersonny.addr
	}
	return "empty http conn"
}

func (c *RhttpConn) newHTTPTransport() *http.Transport {
	tp := &http.Transport{
		// Critical: never pool idle conns. Creating a Transport per request
		// without CloseIdleConnections was a long-lived FD leak under load.
		DisableKeepAlives: true,
	}
	tp.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		if gControlOnConnSetup != nil {
			d = net.Dialer{Control: gControlOnConnSetup}
		}
		return d.DialContext(ctx, network, addr)
	}
	return tp
}

func (c *RhttpConn) postData(ctx context.Context, url string, d []byte) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.checkConfig()

	data := bytes.NewReader(d)
	req, err := http.NewRequestWithContext(ctx, "POST", url, data)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Close = true

	var tp *http.Transport
	ownTP := false
	if c.dialer != nil && c.dialer.tp != nil {
		tp = c.dialer.tp
	} else {
		tp = c.newHTTPTransport()
		ownTP = true
	}
	if ownTP {
		defer tp.CloseIdleConnections()
	}

	timeout := time.Duration(c.config.RequestTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{
		Transport: tp,
		Timeout:   timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, body, nil
}

func (c *RhttpConn) Dial(dst string) (Conn, error) {
	c.checkConfig()

	ctx, cancel := context.WithCancel(context.Background())
	c.closelock.Lock()
	c.cancel = cancel
	c.closelock.Unlock()

	id := common.UniqueId()

	url := dst + "/" + id

	if !strings.HasPrefix(url, "http://") {
		url = "http://" + url
	}

	tp := c.newHTTPTransport()
	tmp := &RhttpConn{config: c.config, dialer: &httpConnDialer{tp: tp, ctx: ctx}}
	code, ret, err := tmp.postData(ctx, url+"?type="+ProtoConnnect, []byte{})
	if err != nil {
		tp.CloseIdleConnections()
		cancel()
		c.closelock.Lock()
		c.cancel = nil
		c.closelock.Unlock()
		return nil, err
	}

	if code != ProtoCodeOK {
		tp.CloseIdleConnections()
		cancel()
		c.closelock.Lock()
		c.cancel = nil
		c.closelock.Unlock()
		return nil, errors.New("dial fail " + string(ret))
	}

	wg := thread.NewGroup("RhttpConn Dialer"+" "+id, nil, nil)

	sendb := list.NewRBuffergo(c.config.BufferSize, true)
	recvb := list.NewRBuffergo(c.config.BufferSize, true)

	dialer := &httpConnDialer{wg: wg, url: url, index: 0, retry: 0, addr: dst, ctx: ctx, tp: tp}

	u := &RhttpConn{id: id, config: c.config, dialer: dialer, sendb: sendb, recvb: recvb, cancel: cancel}

	c.closelock.Lock()
	c.cancel = nil // ownership moved to returned conn
	c.closelock.Unlock()

	wg.Go("RhttpConn updateDialerSonny"+" "+u.Info(), func() error {
		return u.updateDialerSonny()
	})

	return u, nil
}

func (c *RhttpConn) updateDialerSonny() error {

	//loggo.Debug("start http conn %s", c.Info())

	buf := make([]byte, c.config.MaxPacketSize)
	var lastrecv []byte
	var lastsend []byte
	lastrecv = nil
	lastsend = nil
	var fullSince time.Time
	for !c.dialer.wg.IsExit() {
		active := false

		if lastrecv != nil {
			if !c.recvb.Write(lastrecv) {
				time.Sleep(time.Microsecond * 100)
				continue
			}
			active = true
		}
		lastrecv = nil

		var send []byte
		if lastsend == nil {
			sendn := common.MinOfInt(c.sendb.Size(), len(buf))
			if sendn > 0 {
				if !c.sendb.Read(buf[0:sendn]) {
					//loggo.Error("sendb Read fail")
					return errors.New("sendb Read fail")
				}
				active = true
				send = buf[0:sendn]
			}
		} else {
			send = lastsend
			active = true
		}

		ctx := c.dialer.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		code, ret, err := c.postData(ctx, c.dialer.url+"?type="+ProtoData+"&index="+strconv.Itoa(c.dialer.index), send)
		if err != nil || code != ProtoCodeOK {
			if code == ProtoCodeFull {
				// Peer recv buffer full: keep retrying, but bail if stuck too long.
				if fullSince.IsZero() {
					fullSince = time.Now()
				} else {
					limit := time.Duration(c.config.HBTimeoutMs) * time.Millisecond
					if limit <= 0 {
						limit = 10 * time.Second
					}
					if time.Since(fullSince) > limit {
						break
					}
				}
			} else {
				fullSince = time.Time{}
				c.dialer.retry++
				if c.dialer.retry > c.config.MaxRetryNum {
					//loggo.Error("retry max %d", c.dialer.retry)
					break
				}
			}
			lastsend = send
			time.Sleep(time.Millisecond * 100)
			continue
		}
		fullSince = time.Time{}
		lastsend = nil

		//loggo.Debug("dailer send ok %s %d %d %d", c.Info(), c.dialer.index, len(send), len(ret))

		c.dialer.index++
		if c.dialer.index >= c.config.MaxMsgIndex {
			c.dialer.index = 0
		}

		if len(ret) > 0 {
			if !c.recvb.Write(ret) {
				lastrecv = ret
				continue
			}
			active = true
		}

		if !active {
			time.Sleep(time.Microsecond * 100)
		}
	}

	//loggo.Debug("close http conn update %s", c.Info())

	startEndTime := time.Now()
	for !c.dialer.wg.IsExit() {
		now := time.Now()

		diffclose := now.Sub(startEndTime)
		if diffclose > time.Millisecond*time.Duration(c.config.CloseWaitTimeoutMs) {
			break
		}

		if c.recvb.Size() <= 0 {
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	//loggo.Debug("close http conn %s", c.Info())

	// Best-effort ProtoClose if Close() did not already take ownership
	// (e.g. loop exit by error/retry while Close has not run yet).
	if !c.isclose.Load() && c.dialer != nil && c.dialer.url != "" {
		_, _, _ = c.postData(context.Background(), c.dialer.url+"?type="+ProtoClose, []byte{})
	}

	return errors.New("closed")
}

func (c *RhttpConn) Listen(dst string) (Conn, error) {
	c.checkConfig()

	addr, err := net.ResolveTCPAddr("tcp", dst)
	if err != nil {
		return nil, err
	}
	listenerconn, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}

	ch := common.NewChannel(c.config.AcceptChanLen)

	wg := thread.NewGroup("RhttpConn Listen"+" "+dst, nil, func() {
		listenerconn.Close()
		ch.Close()
	})

	listener := &httpConnListener{
		addr:         dst,
		listenerconn: listenerconn,
		wg:           wg,
		accept:       ch,
	}

	u := &RhttpConn{id: common.UniqueId(), config: c.config, listener: listener}
	srv := &http.Server{
		Handler:           u,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	listener.srv = srv
	wg.Go("RhttpConn Listen loopRecv"+" "+dst, func() error {
		return u.loopRecv()
	})
	wg.Go("RhttpConn Listen checkSonnyClose"+" "+dst, func() error {
		return u.checkSonnyClose()
	})

	return u, nil
}

func (c *RhttpConn) Accept() (Conn, error) {
	c.checkConfig()

	if c.listener == nil || c.listener.wg == nil {
		return nil, errors.New("not listen")
	}
	for !c.listener.wg.IsExit() {
		s := <-c.listener.accept.Ch()
		if s == nil {
			break
		}
		sonny := s.(*RhttpConn)
		_, ok := c.listener.sonny.Load(sonny.id)
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

func (c *RhttpConn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//loggo.Debug("ServeHTTP %v %v", r.Method, r.RequestURI)

	u, err := url.Parse(r.RequestURI)
	if err != nil {
		//loggo.Error("Parse fail %v", r.RequestURI)
		w.WriteHeader(ProtoCodeFail)
		w.Write([]byte("url Parse fail"))
		return
	}

	id := u.Path
	param := u.Query()
	types, ok := param["type"]
	if !ok || len(types) == 0 {
		//loggo.Error("no params type %v", r.RequestURI)
		w.WriteHeader(ProtoCodeFail)
		w.Write([]byte("no params type"))
		return
	}
	ty := types[0]

	v, ok := c.listener.sonny.Load(id)
	if !ok {
		if ty != ProtoConnnect {
			//loggo.Error("no sonny id %v", id)
			w.WriteHeader(ProtoCodeFail)
			w.Write([]byte("no sonny id"))
			return
		}

		sonny := &httpConnListenerSonny{
			fwg:          c.listener.wg,
			listener:     c.listener,
			expectIndex:  0,
			lastRecvTime: time.Now(),
			addr:         c.listener.addr,
		}

		sendb := list.NewRBuffergo(c.config.BufferSize, true)
		recvb := list.NewRBuffergo(c.config.BufferSize, true)

		u := &RhttpConn{id: id, config: c.config, listenersonny: sonny, sendb: sendb, recvb: recvb}

		c.listener.sonny.Store(id, u)

		// Non-blocking accept enqueue: blocking here holds the HTTP TCP conn (FD leak under backlog).
		timeoutMs := c.config.RecvChanPushTimeout
		if timeoutMs <= 0 {
			timeoutMs = 100
		}
		if !c.listener.accept.WriteTimeout(u, timeoutMs) {
			c.listener.sonny.Delete(id)
			w.WriteHeader(ProtoCodeFull)
			w.Write([]byte("accept queue full"))
			return
		}

		w.WriteHeader(ProtoCodeOK)

	} else {
		u := v.(*RhttpConn)
		u.listenersonny.mu.Lock()
		defer u.listenersonny.mu.Unlock()

		if u.isclose.Load() {
			w.WriteHeader(ProtoCodeFail)
			w.Write([]byte("sonny closed"))
			return
		}

		u.listenersonny.lastRecvTime = time.Now()

		if ty != ProtoData && ty != ProtoClose {
			//loggo.Error("wrong type %v %v", id, ty)
			w.WriteHeader(ProtoCodeFail)
			w.Write([]byte("wrong type " + ty))
			return
		}

		if ty == ProtoClose {
			u.isclose.Store(true)
			c.listener.sonny.Delete(u.id)
			w.WriteHeader(ProtoCodeOK)
			return
		}

		indexs, ok := param["index"]
		if !ok || len(indexs) == 0 {
			//loggo.Error("no index type %v", r.RequestURI)
			w.WriteHeader(ProtoCodeFail)
			w.Write([]byte("no params index"))
			return
		}
		index, err := strconv.Atoi(indexs[0])
		if err != nil {
			//loggo.Error("index fail %v", r.RequestURI)
			w.WriteHeader(ProtoCodeFail)
			w.Write([]byte("index fail"))
			return
		}

		newrecv := true
		if index != u.listenersonny.expectIndex {
			nextindex := index + 1
			if nextindex >= u.config.MaxMsgIndex {
				nextindex = 0
			}
			if nextindex == u.listenersonny.expectIndex {
				newrecv = false
			} else {
				//loggo.Error("index diff %v %v", r.RequestURI, u.listenersonny.expectIndex)
				w.WriteHeader(ProtoCodeFail)
				w.Write([]byte("index diff"))
				return
			}
		}

		if newrecv {
			maxBody := int64(c.config.MaxPacketSize)
			if maxBody <= 0 {
				maxBody = 1024 * 100
			}
			body, err := ioutil.ReadAll(io.LimitReader(r.Body, maxBody+1))
			if err != nil {
				//loggo.Error("read body fail %v", r.RequestURI)
				w.WriteHeader(ProtoCodeFail)
				w.Write([]byte("read body fail"))
				return
			}
			if int64(len(body)) > maxBody {
				w.WriteHeader(ProtoCodeFail)
				w.Write([]byte("body too large"))
				return
			}

			if !u.recvb.Write(body) {
				//loggo.Debug("body write fail %v %v", r.RequestURI, len(body))
				w.WriteHeader(ProtoCodeFull)
				w.Write([]byte("body write fail"))
				return
			}

			u.listenersonny.expectIndex++
			if u.listenersonny.expectIndex >= u.config.MaxMsgIndex {
				u.listenersonny.expectIndex = 0
			}

			sendn := common.MinOfInt(u.config.MaxPacketSize, u.sendb.Size())
			buff := make([]byte, sendn)
			u.sendb.Read(buff)

			w.WriteHeader(ProtoCodeOK)
			w.Write(buff)

			u.listenersonny.lastSend = buff
		} else {
			w.WriteHeader(ProtoCodeOK)
			w.Write(u.listenersonny.lastSend)
		}
	}
}

func (c *RhttpConn) loopRecv() error {
	c.checkConfig()
	srv := c.listener.srv
	if srv == nil {
		return errors.New("nil http server")
	}
	err := srv.Serve(c.listener.listenerconn)
	if err != nil && !c.listener.wg.IsExit() && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (c *RhttpConn) checkSonnyClose() error {
	c.checkConfig()
	for !c.listener.wg.IsExit() {
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RhttpConn)
			hb := time.Duration(c.config.HBTimeoutMs) * time.Millisecond
			if hb <= 0 {
				hb = 10 * time.Second
			}
			u.listenersonny.mu.Lock()
			expired := u.isclose.Load() || time.Since(u.listenersonny.lastRecvTime) > hb
			u.listenersonny.mu.Unlock()
			if expired {
				u.isclose.Store(true)
				c.listener.sonny.Delete(key)
			}
			return true
		})
		time.Sleep(time.Second)
	}
	return nil
}

func (c *RhttpConn) checkConfig() {
	if c.config == nil {
		c.config = DefaultHttpConfig()
	}
}

func (c *RhttpConn) SetConfig(config *HttpConfig) {
	c.config = config
}

func (c *RhttpConn) GetConfig() *HttpConfig {
	c.checkConfig()
	return c.config
}
