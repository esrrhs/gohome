package dns

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startUDPStub 起一个 UDP DNS 桩：reply=false 时只收不发（用于超时测试）
func startUDPStub(t *testing.T, reply bool) (string, *int32, func()) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Skipf("cannot bind loopback udp: %v", err)
	}
	var queries int32

	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			atomic.AddInt32(&queries, 1)
			if !reply {
				continue
			}
			req := new(dns.Msg)
			if err := req.Unpack(buf[:n]); err != nil || len(req.Question) == 0 {
				continue
			}
			resp := new(dns.Msg)
			resp.SetReply(req)
			rr, rrErr := dns.NewRR(req.Question[0].Name + " 600 IN A 203.0.113.10")
			if rrErr != nil {
				continue
			}
			resp.Answer = append(resp.Answer, rr)
			wire, packErr := resp.Pack()
			if packErr != nil {
				continue
			}
			if _, err := conn.WriteTo(wire, addr); err != nil {
				return
			}
		}
	}()

	return conn.LocalAddr().String(), &queries, func() { _ = conn.Close() }
}

// TestResolver_TimeoutAppliesToDirectPath 回归测试：
// 此前只有并发竞速分支套用了 cfg.Timeout，命中直连白名单时超时配置被静默忽略。
func TestResolver_TimeoutAppliesToDirectPath(t *testing.T) {
	addr, _, stop := startUDPStub(t, false) // 永不回包
	defer stop()

	cfg := DefaultConfig()
	cfg.DirectUpstreams = []string{addr}
	cfg.RemoteUpstreams = nil
	cfg.Timeout = 150 * time.Millisecond
	cfg.DirectDomains = []string{"slow.test"}

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	start := time.Now()
	_, err = r.Resolve(context.Background(), "slow.test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected a timeout error from a silent upstream")
	}
	// 走修复前的路径会等到上游默认的 4s；配置 150ms 时应当在 2s 内失败
	if elapsed > 2*time.Second {
		t.Fatalf("cfg.Timeout was ignored: query took %v", elapsed)
	}
}

func TestResolver_CachesRepeatedQueries(t *testing.T) {
	addr, queries, stop := startUDPStub(t, true)
	defer stop()

	cfg := DefaultConfig()
	cfg.DirectUpstreams = []string{addr}
	cfg.RemoteUpstreams = nil
	cfg.DirectDomains = []string{"cached.test"}

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	first, err := r.Resolve(ctx, "cached.test")
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if len(first) == 0 || first[0].String() != "203.0.113.10" {
		t.Fatalf("unexpected ips: %v", first)
	}

	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(ctx, "cached.test"); err != nil {
			t.Fatalf("cached resolve failed: %v", err)
		}
	}
	if got := atomic.LoadInt32(queries); got != 1 {
		t.Fatalf("expected 1 upstream query thanks to the cache, got %d", got)
	}

	r.ClearCache()
	if _, err := r.Resolve(ctx, "cached.test"); err != nil {
		t.Fatalf("resolve after ClearCache failed: %v", err)
	}
	if got := atomic.LoadInt32(queries); got != 2 {
		t.Fatalf("expected a fresh upstream query after ClearCache, got %d", got)
	}
}

// TestResolver_FakeIPCustomRange 回归测试：
// 判定此前写死 198.18/198.19，自定义 Fake-IP 网段时会全部判错。
func TestResolver_FakeIPCustomRange(t *testing.T) {
	const custom = "10.66.0.0/16"
	cfg := DefaultConfig()
	cfg.EnableFakeIP = true
	cfg.FakeIPRange = custom

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	ip, err := r.ResolveOne(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if ip == nil {
		t.Fatalf("expected an IP")
	}
	if !strings.HasPrefix(ip.String(), "10.66.") {
		t.Fatalf("expected an IP from %s, got %v", custom, ip)
	}
	if !r.IsFakeIP(ip) {
		t.Fatalf("IsFakeIP(%v) = false for the configured custom range", ip)
	}
	if proxy, err := r.ShouldProxy(ip.String()); err != nil || !proxy {
		t.Fatalf("ShouldProxy(fake ip) = (%v, %v), want (true, nil)", proxy, err)
	}

	if _, ok := r.LookupDomainByFakeIPStr(ip.String()); !ok {
		t.Fatalf("expected reverse lookup to succeed for %v", ip)
	}
}

func TestResolver_HotUpdateKeepsBuiltinDefaults(t *testing.T) {
	r, err := NewResolver(DefaultConfig())
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	// 更新用户网段后，内置保留网段必须仍然生效（此前会被整段覆盖掉）
	r.UpdateDirectIPs([]string{"1.2.3.4/32"})
	for _, ip := range []string{"1.2.3.4", "192.168.1.1", "127.0.0.1", "10.0.0.1"} {
		if !r.directIPs.Contains(net.ParseIP(ip)) {
			t.Fatalf("expected %s to stay direct after UpdateDirectIPs", ip)
		}
	}
	if r.directIPs.Contains(net.ParseIP("8.8.8.8")) {
		t.Fatalf("8.8.8.8 must not be direct")
	}

	// 更新用户域名后，内置默认直连域名必须仍然命中
	r.UpdateDirectDomains([]string{"mycompany.internal"})
	for _, d := range []string{"mycompany.internal", "baidu.com", "api.weixin.com", "gov.cn"} {
		if !r.directDomains.Has(d) {
			t.Fatalf("expected %s to stay direct after UpdateDirectDomains", d)
		}
	}

	r.UpdateProxyDomains([]string{"blocked.example"})
	if !r.proxyDomains.Has("blocked.example") {
		t.Fatalf("expected blocked.example to be a proxy domain")
	}
}

func TestResolver_ShouldProxyWithoutNetwork(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DirectDomains = []string{"internal.corp"}
	cfg.ProxyDomains = []string{"blocked.example"}

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	cases := []struct {
		in   string
		want bool
	}{
		{"internal.corp", false},
		{"blocked.example", true},
		{"192.168.1.1", false},
		{"127.0.0.1", false},
		{"", false},
	}
	for _, c := range cases {
		got, err := r.ShouldProxy(c.in)
		if err != nil {
			t.Fatalf("ShouldProxy(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("ShouldProxy(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestResolver_ExchangeRejectsEmptyQuestion(t *testing.T) {
	r, err := NewResolver(DefaultConfig())
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	if _, err := r.Exchange(context.Background(), nil); err == nil {
		t.Fatalf("expected an error for a nil request")
	}
	if _, err := r.Exchange(context.Background(), new(dns.Msg)); err == nil {
		t.Fatalf("expected an error for a request without questions")
	}
}

func TestResolver_InvalidUpstreams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DirectUpstreams = []string{"223.5.5.5:notaport"}
	cfg.RemoteUpstreams = nil

	if _, err := NewResolver(cfg); err == nil {
		t.Fatalf("expected NewResolver to fail when every direct upstream is invalid")
	}
}

func TestResolver_ResolveLiteralIP(t *testing.T) {
	r, err := NewResolver(DefaultConfig())
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	defer r.Close()

	ips, err := r.Resolve(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "203.0.113.9" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

// TestResolver_ARPAHelper 覆盖 in-addr.arpa 反解地址的辅助函数
func TestResolver_ARPAHelper(t *testing.T) {
	cases := map[string]string{
		"1.0.0.127.in-addr.arpa": "127.0.0.1",
		"4.3.2.1.in-addr.arpa":   "1.2.3.4",
		"4.3.2.1.in-addr.arpa.":  "1.2.3.4",
		"not-an-arpa":            "",
		"1.0.0.in-addr.arpa":     "",
		"a.b.c.d.in-addr.arpa":   "",
		"a.b.c.d.e.in-addr.arpa": "",
		"999.1.2.3.in-addr.arpa": "",
	}
	for in, want := range cases {
		if got := arpaToIPv4(in); got != want {
			t.Fatalf("arpaToIPv4(%q) = %q, want %q", in, got, want)
		}
	}
}
