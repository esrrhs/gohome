package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// SocketUpstream 实现了基于 UDP 或 TCP 的传统 DNS 上游
type SocketUpstream struct {
	addr    string
	network string
	client  *dns.Client
}

// NewSocketUpstream 创建一个 UDP/TCP 上游（例如 "223.5.5.5:53" 或 "tcp://223.5.5.5:53"）
func NewSocketUpstream(addr string) (*SocketUpstream, error) {
	network := "udp"
	cleanAddr := strings.TrimSpace(addr)
	if strings.HasPrefix(cleanAddr, "tcp://") {
		network = "tcp"
		cleanAddr = strings.TrimPrefix(cleanAddr, "tcp://")
	} else if strings.HasPrefix(cleanAddr, "udp://") {
		network = "udp"
		cleanAddr = strings.TrimPrefix(cleanAddr, "udp://")
	}

	if cleanAddr == "" {
		return nil, errors.New("empty upstream address")
	}

	// 未显式带端口时补默认 53
	if !hasPort(cleanAddr) {
		cleanAddr = net.JoinHostPort(cleanAddr, "53")
	}

	host, port, err := net.SplitHostPort(cleanAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream address %q: %w", addr, err)
	}
	if host == "" {
		return nil, fmt.Errorf("invalid upstream address %q: empty host", addr)
	}
	if _, err := net.LookupPort(network, port); err != nil {
		return nil, fmt.Errorf("invalid upstream address %q: bad port %q", addr, port)
	}

	return &SocketUpstream{
		addr:    cleanAddr,
		network: network,
		client: &dns.Client{
			Net:     network,
			Timeout: DefaultTimeout,
		},
	}, nil
}

// hasPort 判断地址是否已经带了端口。
// IPv6 字面量必须写成 "[2001:db8::1]:53"，裸的 "2001:db8::1" 会被判为非法地址。
func hasPort(addr string) bool {
	if strings.HasPrefix(addr, "[") {
		i := strings.LastIndex(addr, "]")
		return i >= 0 && i+1 < len(addr) && addr[i+1] == ':'
	}
	return strings.Contains(addr, ":")
}

func (s *SocketUpstream) Address() string {
	return fmt.Sprintf("%s://%s", s.network, s.addr)
}

func (s *SocketUpstream) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	deadline, ok := ctx.Deadline()
	client := s.client
	if ok {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			return nil, context.DeadlineExceeded
		}
		client = &dns.Client{
			Net:     s.network,
			Timeout: timeout,
		}
	}

	resp, _, err := client.Exchange(req, s.addr)
	return resp, err
}
