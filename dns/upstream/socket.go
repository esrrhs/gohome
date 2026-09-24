package upstream

import (
	"context"
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
	cleanAddr := addr
	if strings.HasPrefix(addr, "tcp://") {
		network = "tcp"
		cleanAddr = strings.TrimPrefix(addr, "tcp://")
	} else if strings.HasPrefix(addr, "udp://") {
		network = "udp"
		cleanAddr = strings.TrimPrefix(addr, "udp://")
	}

	if !strings.Contains(cleanAddr, ":") {
		cleanAddr = net.JoinHostPort(cleanAddr, "53")
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
