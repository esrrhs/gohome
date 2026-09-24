package dns

import (
	"context"
	"fmt"
	"sync"

	"github.com/esrrhs/gohome/loggo"
	"github.com/miekg/dns"
)

// Server 提供可运行的 DNS 服务器监听端（支持 UDP / TCP）
type Server struct {
	addr       string
	resolver   Resolver
	udpServer  *dns.Server
	tcpServer  *dns.Server
	mu         sync.Mutex
	stopSignal chan struct{}
}

// NewServer 创建 DNS 服务端
func NewServer(addr string, resolver Resolver) *Server {
	if addr == "" {
		addr = ":53"
	}
	return &Server{
		addr:       addr,
		resolver:   resolver,
		stopSignal: make(chan struct{}),
	}
}

// Start 启动 DNS 监听
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp, err := s.resolver.Exchange(context.Background(), r)
		if err != nil || resp == nil {
			dns.HandleFailed(w, r)
			return
		}
		resp.Id = r.Id
		_ = w.WriteMsg(resp)
	})

	s.udpServer = &dns.Server{
		Addr:    s.addr,
		Net:     "udp",
		Handler: handler,
	}

	s.tcpServer = &dns.Server{
		Addr:    s.addr,
		Net:     "tcp",
		Handler: handler,
	}

	errChan := make(chan error, 2)
	go func() {
		loggo.Info("[DNS Server] Listening UDP on %s", s.addr)
		if err := s.udpServer.ListenAndServe(); err != nil {
			errChan <- fmt.Errorf("udp listen error: %w", err)
		}
	}()

	go func() {
		loggo.Info("[DNS Server] Listening TCP on %s", s.addr)
		if err := s.tcpServer.ListenAndServe(); err != nil {
			errChan <- fmt.Errorf("tcp listen error: %w", err)
		}
	}()

	// 快速探测是否有即时启动错误
	select {
	case err := <-errChan:
		_ = s.Stop()
		return err
	default:
		return nil
	}
}

// Stop 停止服务
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errUDP, errTCP error
	if s.udpServer != nil {
		errUDP = s.udpServer.Shutdown()
		s.udpServer = nil
	}
	if s.tcpServer != nil {
		errTCP = s.tcpServer.Shutdown()
		s.tcpServer = nil
	}
	if errUDP != nil {
		return errUDP
	}
	return errTCP
}
