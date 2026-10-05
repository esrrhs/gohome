package dns

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/loggo"
	"github.com/miekg/dns"
)

const (
	// serverStartTimeout 限制等待两个监听器完成绑定（或绑定失败）的时间，
	// 避免某个监听器卡在 ListenAndServe 内部时 Start 永久阻塞。
	serverStartTimeout = 3 * time.Second
	// serverStopTimeout 限制 Shutdown 等待存量连接收尾的时间。
	serverStopTimeout = 5 * time.Second
)

// Server 提供可运行的 DNS 服务器监听端（支持 UDP / TCP）
type Server struct {
	addr      string
	resolver  Resolver
	udpServer *dns.Server
	tcpServer *dns.Server
	mu        sync.Mutex
}

// NewServer 创建 DNS 服务端
func NewServer(addr string, resolver Resolver) *Server {
	if addr == "" {
		addr = ":53"
	}
	return &Server{
		addr:     addr,
		resolver: resolver,
	}
}

// Addr 返回当前监听地址
func (s *Server) Addr() string {
	return s.addr
}

// Start 启动 DNS 监听
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.udpServer != nil || s.tcpServer != nil {
		return errors.New("dns server already started")
	}
	if s.resolver == nil {
		return errors.New("dns server requires a non-nil resolver")
	}

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp, err := s.resolver.Exchange(context.Background(), r)
		if err != nil || resp == nil {
			dns.HandleFailed(w, r)
			return
		}
		resp.Id = r.Id
		_ = w.WriteMsg(resp)
	})

	// 每个监听器最终必然落到两种结局之一：绑定成功（NotifyStartedFunc 被回调）
	// 或绑定失败（ListenAndServe 返回 error）。两种结局都会把 pending 计数减一，
	// 因此 pending.Wait() 一定会返回，Start 不会永久阻塞。
	var pending sync.WaitGroup
	pending.Add(2)
	var startCount int32

	// newNotify 返回一对闭包：notify 由启动回调触发，decided 由 goroutine 退出时触发。
	// 二者共享同一个 sync.Once，保证 pending 只会被 Done 一次。
	newNotify := func() (notify func(), decided func()) {
		var once sync.Once
		done := func() { once.Do(pending.Done) }
		return func() {
			atomic.AddInt32(&startCount, 1)
			done()
		}, done
	}

	udpNotify, udpDecided := newNotify()
	tcpNotify, tcpDecided := newNotify()

	// 容量 2：即使 Start 返回后不再读取，两个 goroutine 也能正常退出而不泄漏。
	errChan := make(chan error, 2)

	s.udpServer = &dns.Server{
		Addr:              s.addr,
		Net:               "udp",
		Handler:           handler,
		NotifyStartedFunc: udpNotify,
	}
	s.tcpServer = &dns.Server{
		Addr:              s.addr,
		Net:               "tcp",
		Handler:           handler,
		NotifyStartedFunc: tcpNotify,
	}

	go func() {
		defer udpDecided()
		if err := s.udpServer.ListenAndServe(); err != nil {
			errChan <- fmt.Errorf("udp listen on %s: %w", s.addr, err)
		}
	}()

	go func() {
		defer tcpDecided()
		if err := s.tcpServer.ListenAndServe(); err != nil {
			errChan <- fmt.Errorf("tcp listen on %s: %w", s.addr, err)
		}
	}()

	waitDone := make(chan struct{})
	go func() {
		pending.Wait()
		close(waitDone)
	}()

	timer := time.NewTimer(serverStartTimeout)
	defer timer.Stop()

	timedOut := false
	select {
	case <-waitDone:
	case <-timer.C:
		timedOut = true
		loggo.Warn("[DNS Server] start probe timed out after %v", serverStartTimeout)
	}

	var firstErr error
	select {
	case firstErr = <-errChan:
	default:
	}

	if firstErr == nil {
		if n := atomic.LoadInt32(&startCount); n < 2 {
			if timedOut {
				firstErr = fmt.Errorf("dns server start timed out after %v: %d/2 listeners serving on %s",
					serverStartTimeout, n, s.addr)
			} else {
				firstErr = fmt.Errorf("dns server start incomplete: %d/2 listeners serving on %s", n, s.addr)
			}
		}
	}
	if firstErr != nil {
		// 注意：此处仍持有 s.mu，必须调用无锁版本，直接调 Stop 会自死锁。
		s.shutdownLocked()
		return firstErr
	}

	loggo.Info("[DNS Server] Listening on %s (udp+tcp)", s.addr)
	return nil
}

// Stop 停止服务
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownLocked()
	return nil
}

// shutdownLocked 关闭 UDP 与 TCP 监听器，调用方必须已持有 s.mu
func (s *Server) shutdownLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), serverStopTimeout)
	defer cancel()

	if s.udpServer != nil {
		if err := s.udpServer.ShutdownContext(ctx); err != nil {
			loggo.Debug("[DNS Server] udp shutdown: %v", err)
		}
		s.udpServer = nil
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.ShutdownContext(ctx); err != nil {
			loggo.Debug("[DNS Server] tcp shutdown: %v", err)
		}
		s.tcpServer = nil
	}
}
