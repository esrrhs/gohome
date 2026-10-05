package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// stubResolver 实现一个可控的 Resolver，供 Server 测试使用
type stubResolver struct {
	ip      net.IP
	wantErr bool
	asked   chan string
}

func (s *stubResolver) Resolve(ctx context.Context, domain string) ([]net.IP, error) {
	if s.wantErr {
		return nil, errors.New("stub resolve failure")
	}
	return []net.IP{s.ip}, nil
}

func (s *stubResolver) ResolveOne(ctx context.Context, domain string) (net.IP, error) {
	if s.wantErr {
		return nil, errors.New("stub resolve failure")
	}
	return s.ip, nil
}

func (s *stubResolver) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	if s.asked != nil {
		select {
		case s.asked <- req.Question[0].Name:
		default:
		}
	}
	if s.wantErr {
		return nil, errors.New("stub exchange failure")
	}
	resp := new(dns.Msg)
	resp.SetReply(req)
	if len(req.Question) > 0 && req.Question[0].Qtype == dns.TypeA {
		rr, err := dns.NewRR(fmt.Sprintf("%s 60 IN A %s", req.Question[0].Name, s.ip))
		if err == nil {
			resp.Answer = append(resp.Answer, rr)
		}
	}
	return resp, nil
}

func (s *stubResolver) ShouldProxy(domainOrIP string) (bool, error)   { return false, nil }
func (s *stubResolver) LookupDomainByFakeIP(ip net.IP) (string, bool) { return "", false }
func (s *stubResolver) LookupDomainByFakeIPStr(ipStr string) (string, bool) {
	return "", false
}
func (s *stubResolver) IsFakeIP(ip net.IP) bool              { return false }
func (s *stubResolver) UpdateDirectIPs(cidrs []string) int   { return 0 }
func (s *stubResolver) UpdateDirectDomains(domains []string) {}
func (s *stubResolver) UpdateProxyDomains(domains []string)  {}
func (s *stubResolver) LoadDirectDomainFile(p string) (int, error) {
	return 0, nil
}
func (s *stubResolver) LoadProxyDomainFile(p string) (int, error) { return 0, nil }
func (s *stubResolver) ReloadGeoIPDatabase(p string) error        { return nil }
func (s *stubResolver) UpdateProxyAddress(a string) error         { return nil }
func (s *stubResolver) ClearCache()                               {}

// freePort 取一个当前空闲的回环端口
func freePort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind loopback: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()
	return port
}

func queryLoopback(t *testing.T, addr string) (*dns.Msg, error) {
	t.Helper()
	c := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	resp, _, err := c.Exchange(m, addr)
	return resp, err
}

func TestServerStartStopAndQuery(t *testing.T) {
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	asked := make(chan string, 4)
	srv := NewServer(addr, &stubResolver{ip: net.ParseIP("9.9.9.9"), asked: asked})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	if srv.Addr() != addr {
		t.Fatalf("Addr() = %q, want %q", srv.Addr(), addr)
	}

	resp, err := queryLoopback(t, addr)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.ParseIP("9.9.9.9")) {
		t.Fatalf("unexpected answer: %v", resp.Answer[0])
	}
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatalf("resolver was never called")
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	// Stop 必须幂等
	if err := srv.Stop(); err != nil {
		t.Fatalf("second Stop failed: %v", err)
	}
}

func TestServerStartTwice(t *testing.T) {
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	srv := NewServer(addr, &stubResolver{ip: net.ParseIP("9.9.9.9")})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	if err := srv.Start(); err == nil {
		t.Fatalf("expected the second Start to fail")
	}
}

func TestServerRequiresResolver(t *testing.T) {
	srv := NewServer(fmt.Sprintf("127.0.0.1:%d", freePort(t)), nil)
	if err := srv.Start(); err == nil {
		_ = srv.Stop()
		t.Fatalf("expected Start to fail with a nil resolver")
	}
}

// TestServerStartFailureDoesNotHang 是死锁回归测试。
// 旧实现在 Start 持有 s.mu 的情况下调用 Stop（Stop 会再次 Lock），
// 一旦监听器绑定失败就会永久卡死。这里要求 Start 必须带错误快速返回，
// 且失败之后 Stop 依然能正常完成。
func TestServerStartFailureDoesNotHang(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: binding port 53 would succeed")
	}

	// 53 是特权端口，非 root 绑定必然失败
	srv := NewServer("127.0.0.1:53", &stubResolver{ip: net.ParseIP("9.9.9.9")})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	select {
	case err := <-errCh:
		if err == nil {
			_ = srv.Stop()
			t.Skip("port 53 was bindable in this environment")
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("Start hung on a bind failure: this is the self-deadlock regression")
	}

	// 失败之后锁必须已经释放，Stop 不能再卡住
	stopCh := make(chan error, 1)
	go func() { stopCh <- srv.Stop() }()
	select {
	case <-stopCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("Stop hung after a failed Start: mutex was left locked")
	}
}

// TestServerReturnsServFailOnResolverError 上游出错时服务端应回 SERVFAIL 而不是静默丢包
func TestServerReturnsServFailOnResolverError(t *testing.T) {
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	srv := NewServer(addr, &stubResolver{wantErr: true})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	resp, err := queryLoopback(t, addr)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if resp.Rcode != dns.RcodeServerFailure {
		t.Fatalf("expected SERVFAIL, got rcode %d", resp.Rcode)
	}
}
