package dns

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/esrrhs/gohome/dns/upstream"
	"github.com/miekg/dns"
)

// startUDPHandlerStub 起一个按 qtype 可编程应答的 UDP DNS 桩：
// handler 返回 nil 表示丢弃该请求（模拟静默上游），返回 SERVFAIL 等也可自由构造。
func startUDPHandlerStub(t *testing.T, handler func(req *dns.Msg) *dns.Msg) (string, *int32, func()) {
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
			req := new(dns.Msg)
			if err := req.Unpack(buf[:n]); err != nil || len(req.Question) == 0 {
				continue
			}
			resp := handler(req)
			if resp == nil {
				continue
			}
			wire, err := resp.Pack()
			if err != nil {
				continue
			}
			if _, err := conn.WriteTo(wire, addr); err != nil {
				return
			}
		}
	}()

	return conn.LocalAddr().String(), &queries, func() { _ = conn.Close() }
}

// replyA 构造只返回一条 A 记录（或空应答）的处理器。
func replyA(ip string) func(*dns.Msg) *dns.Msg {
	return func(req *dns.Msg) *dns.Msg {
		resp := new(dns.Msg)
		resp.SetReply(req)
		if ip != "" {
			if rr, err := dns.NewRR(req.Question[0].Name + " 600 IN A " + ip); err == nil {
				resp.Answer = append(resp.Answer, rr)
			}
		}
		return resp
	}
}

// dualStackHandler 对 A/AAAA 分别返回指定记录，其余类型空应答。
func dualStackHandler(aIP, aaaaIP string) func(*dns.Msg) *dns.Msg {
	return func(req *dns.Msg) *dns.Msg {
		resp := new(dns.Msg)
		resp.SetReply(req)
		q := req.Question[0]
		var rr dns.RR
		var err error
		switch q.Qtype {
		case dns.TypeA:
			if aIP != "" {
				rr, err = dns.NewRR(q.Name + " 600 IN A " + aIP)
			}
		case dns.TypeAAAA:
			if aaaaIP != "" {
				rr, err = dns.NewRR(q.Name + " 600 IN AAAA " + aaaaIP)
			}
		}
		if err == nil && rr != nil {
			resp.Answer = append(resp.Answer, rr)
		}
		return resp
	}
}

func servFailHandler(req *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Rcode = dns.RcodeServerFailure
	return resp
}

// startDoHStub 起一个本地 DoH (RFC 8484 POST) 桩。
func startDoHStub(t *testing.T, handler func(req *dns.Msg) *dns.Msg) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		req := new(dns.Msg)
		if err := req.Unpack(body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := handler(req)
		if resp == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		wire, err := resp.Pack()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(wire)
	}))
	return srv, srv.Close
}

// newStubResolver 用给定直连/远程上游构造 resolver，不触达任何外网。
func newStubResolver(t *testing.T, direct, remote []string, extra ...func(*Config)) *StandardResolver {
	t.Helper()
	cfg := Config{
		DirectUpstreams: direct,
		RemoteUpstreams: remote,
		Timeout:         2 * time.Second,
		CacheCapacity:   64,
	}
	for _, fn := range extra {
		fn(&cfg)
	}
	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestResolver_ResolveAll_DualStack(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, dualStackHandler("10.0.0.10", "2001:db8::10"))
	defer stop()

	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"})

	ips, err := r.ResolveAll(context.Background(), "dual.test")
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}
	if len(ips) != 2 {
		t.Fatalf("expected 2 IPs (A+AAAA), got %d: %v", len(ips), ips)
	}
	// IPv4 必须排在 IPv6 前面
	if ips[0].To4() == nil {
		t.Fatalf("expected IPv4 first, got %v", ips)
	}
	if ips[1].To4() != nil {
		t.Fatalf("expected IPv6 second, got %v", ips)
	}
	if ips[0].String() != "10.0.0.10" || ips[1].String() != "2001:db8::10" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

func TestResolver_ResolveAll_AOnly(t *testing.T) {
	// 上游对 AAAA 返回无 Answer 的合法响应
	addr, _, stop := startUDPHandlerStub(t, dualStackHandler("10.0.0.11", ""))
	defer stop()

	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"})

	ips, err := r.ResolveAll(context.Background(), "aonly.test")
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.11" {
		t.Fatalf("expected only the A record, got %v", ips)
	}
}

func TestResolver_ResolveAll_LiteralIP(t *testing.T) {
	addr, queries, stop := startUDPHandlerStub(t, dualStackHandler("10.0.0.12", ""))
	defer stop()

	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"})

	ips, err := r.ResolveAll(context.Background(), "198.51.100.7")
	if err != nil {
		t.Fatalf("ResolveAll literal IP failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.7" {
		t.Fatalf("literal IP passthrough broken: %v", ips)
	}
	if atomic.LoadInt32(queries) != 0 {
		t.Fatalf("literal IP must not trigger upstream queries, got %d", *queries)
	}
}

// 命中代理域名白名单时只查远程上游，直连上游一个请求都不能收到。
func TestResolver_QueryRemote_ProxyDomain(t *testing.T) {
	directAddr, directQueries, stopDirect := startUDPHandlerStub(t, replyA("10.0.0.20"))
	defer stopDirect()
	remoteAddr, remoteQueries, stopRemote := startUDPHandlerStub(t, replyA("198.51.100.20"))
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr}, func(c *Config) {
		c.ProxyDomains = []string{"remote.test"}
	})

	ips, err := r.Resolve(context.Background(), "remote.test")
	if err != nil {
		t.Fatalf("resolve via remote failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.20" {
		t.Fatalf("expected remote upstream answer, got %v", ips)
	}
	if atomic.LoadInt32(directQueries) != 0 {
		t.Fatalf("direct upstream must not be queried for a proxy domain, got %d", *directQueries)
	}
	if atomic.LoadInt32(remoteQueries) == 0 {
		t.Fatal("remote upstream was never queried")
	}
}

// 远程上游不可用时，代理域名回退直连。构造时给一个非法的远程地址，
// initUpstreams 会跳过它，得到空的远程列表。
func TestResolver_QueryRemote_NoRemoteFallbackDirect(t *testing.T) {
	directAddr, directQueries, stopDirect := startUDPHandlerStub(t, replyA("10.0.0.21"))
	defer stopDirect()

	r := newStubResolver(t, []string{directAddr}, []string{":53"}, func(c *Config) {
		c.ProxyDomains = []string{"fallback.test"}
	})

	ips, err := r.Resolve(context.Background(), "fallback.test")
	if err != nil {
		t.Fatalf("expected fallback to direct upstream, got error: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.21" {
		t.Fatalf("unexpected fallback answer: %v", ips)
	}
	if atomic.LoadInt32(directQueries) == 0 {
		t.Fatal("expected the direct upstream to serve the fallback query")
	}
}

// 竞速路径：直连返回保留网段 IP，未加载 GeoIP 时应直接采纳直连结果。
func TestResolver_QueryParallel_AcceptDomesticDirect(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, replyA("10.1.2.3"))
	defer stopDirect()
	remoteAddr, remoteQueries, stopRemote := startUDPHandlerStub(t, replyA("198.51.100.30"))
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr})

	ips, err := r.Resolve(context.Background(), "domestic-parallel.test")
	if err != nil {
		t.Fatalf("parallel resolve failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "10.1.2.3" {
		t.Fatalf("expected domestic direct answer, got %v", ips)
	}
	// 直连结果可信时不等待远程；远程至多收到在途请求，不校验精确计数，
	// 只确保采纳的答案不是远程的（上面已断言）。
	_ = remoteQueries
}

// 竞速路径：直连快速失败（SERVFAIL 无 Answer），远程正常，应采纳远程结果。
func TestResolver_QueryParallel_RemoteFallback(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, servFailHandler)
	defer stopDirect()
	remoteAddr, _, stopRemote := startUDPHandlerStub(t, replyA("198.51.100.40"))
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr})

	ips, err := r.Resolve(context.Background(), "fallback-parallel.test")
	if err != nil {
		t.Fatalf("expected remote answer after direct failure: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.40" {
		t.Fatalf("expected remote answer, got %v", ips)
	}
}

// 竞速路径：两侧都失败时返回错误。
func TestResolver_QueryParallel_BothFail(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, servFailHandler)
	defer stopDirect()
	remoteAddr, _, stopRemote := startUDPHandlerStub(t, servFailHandler)
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr})

	if _, err := r.Resolve(context.Background(), "all-fail.test"); err == nil {
		t.Fatal("expected an error when both upstreams fail")
	}
}

func TestResolver_IsAllDomestic(t *testing.T) {
	r := newStubResolver(t, []string{"127.0.0.1:1"}, []string{"127.0.0.1:2"})

	// 内置保留网段（含 RFC1918 私有地址）直接视为国内直连
	for _, s := range []string{"10.0.0.1", "172.16.5.4", "192.168.9.9", "127.0.0.1"} {
		if !r.isAllDomestic([]net.IP{net.ParseIP(s)}) {
			t.Errorf("expected %s to be domestic (reserved range)", s)
		}
	}
	// 未加载 GeoIP 时，公网 IP 查不到国家码，按现有逻辑放行（视为国内）
	if !r.isAllDomestic([]net.IP{net.ParseIP("203.0.113.50")}) {
		t.Error("public IP without GeoIP loaded should be treated as domestic")
	}
}

// ShouldProxy 对未命中规则的域名会实际解析后按 IP 归属判定。
func TestResolver_ShouldProxyResolvesDomain(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, replyA("203.0.113.60"))
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"})

	proxy, err := r.ShouldProxy("unmatched.example")
	if err != nil {
		t.Fatalf("ShouldProxy error: %v", err)
	}
	if proxy {
		t.Fatal("unmatched domain resolving to a public IP without GeoIP should be direct (false)")
	}
}

// 上游静默时 ShouldProxy 应在 cfg.Timeout 内失败并默认建议走代理。
func TestResolver_ShouldProxyResolveFailure(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(*dns.Msg) *dns.Msg { return nil })
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.Timeout = 150 * time.Millisecond
	})

	start := time.Now()
	proxy, err := r.ShouldProxy("dead.example")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected resolution failure to surface as error")
	}
	if !proxy {
		t.Fatal("resolution failure should default to proxy=true")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ShouldProxy ignored cfg.Timeout, took %v", elapsed)
	}
}

func TestResolver_ResolveOneFailure(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(*dns.Msg) *dns.Msg { return nil })
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.Timeout = 150 * time.Millisecond
		c.DirectDomains = []string{"dead-direct.test"}
	})

	if _, err := r.ResolveOne(context.Background(), "dead-direct.test"); err == nil {
		t.Fatal("expected ResolveOne to propagate the upstream failure")
	}
}

func TestResolver_LoadDomainFiles(t *testing.T) {
	dir := t.TempDir()
	directFile := filepath.Join(dir, "direct.conf")
	proxyFile := filepath.Join(dir, "proxy.conf")
	content := strings.Join([]string{
		"# comment line",
		"",
		"server=/from-file-direct.test/114.114.114.114",
		"plain-direct.test",
		"ipset=/more-direct.test/",
	}, "\n")
	if err := os.WriteFile(directFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxyFile, []byte("from-file-proxy.test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	addr, _, stop := startUDPHandlerStub(t, replyA("10.0.0.30"))
	defer stop()

	// 同时覆盖 NewResolver 构造期加载规则文件与不存在的 GeoIP 路径跳过分支
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.DirectDomainFiles = []string{directFile}
		c.ProxyDomainFiles = []string{proxyFile}
		c.GeoIPFile = filepath.Join(dir, "missing.mmdb")
	})

	if !r.directDomains.Has("from-file-direct.test") ||
		!r.directDomains.Has("plain-direct.test") ||
		!r.directDomains.Has("more-direct.test") {
		t.Fatal("direct domains loaded from file at construction are missing")
	}
	if !r.proxyDomains.Has("from-file-proxy.test") {
		t.Fatal("proxy domain loaded from file at construction is missing")
	}

	// 运行期再追加加载
	extra := filepath.Join(dir, "extra.conf")
	if err := os.WriteFile(extra, []byte("runtime-direct.test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := r.LoadDirectDomainFile(extra)
	if err != nil || n != 1 {
		t.Fatalf("LoadDirectDomainFile = (%d, %v), want (1, nil)", n, err)
	}
	if !r.directDomains.Has("runtime-direct.test") {
		t.Fatal("runtime-loaded direct domain missing")
	}

	n, err = r.LoadProxyDomainFile(extra)
	if err != nil || n != 1 {
		t.Fatalf("LoadProxyDomainFile = (%d, %v), want (1, nil)", n, err)
	}

	if _, err := r.LoadDirectDomainFile(filepath.Join(dir, "no-such-file")); err == nil {
		t.Fatal("expected error when loading a nonexistent domain file")
	}
}

func TestResolver_ReloadGeoIPDatabaseMissing(t *testing.T) {
	r := newStubResolver(t, []string{"127.0.0.1:1"}, []string{"127.0.0.1:2"})
	if err := r.ReloadGeoIPDatabase(filepath.Join(t.TempDir(), "no.mmdb")); err == nil {
		t.Fatal("expected error when reloading a nonexistent GeoIP database")
	}
}

// UpdateProxyAddress 会重建全部上游：旧的 DoH 上游必须被 Close，
// 重建后解析仍正常。
func TestResolver_UpdateProxyAddressRebuild(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, replyA("10.0.0.40"))
	defer stopDirect()
	dohSrv, stopDoH := startDoHStub(t, replyA("198.51.100.50"))
	defer stopDoH()

	r := newStubResolver(t, []string{directAddr}, []string{dohSrv.URL}, func(c *Config) {
		c.DirectDomains = []string{"after-rebuild.test"}
	})

	if err := r.UpdateProxyAddress(""); err != nil {
		t.Fatalf("UpdateProxyAddress failed: %v", err)
	}
	// 再次重建：旧 DoH client 的空闲连接会被关闭，不应报错
	if err := r.UpdateProxyAddress(""); err != nil {
		t.Fatalf("second UpdateProxyAddress failed: %v", err)
	}

	// 重建后直连域名解析不受影响
	ips, err := r.Resolve(context.Background(), "after-rebuild.test")
	if err != nil {
		t.Fatalf("resolve after proxy rebuild failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.40" {
		t.Fatalf("unexpected ips after rebuild: %v", ips)
	}
}

func TestResolver_LookupFakeIPWhenDisabled(t *testing.T) {
	r := newStubResolver(t, []string{"127.0.0.1:1"}, []string{"127.0.0.1:2"})
	if d, ok := r.LookupDomainByFakeIP(net.ParseIP("198.18.0.1")); ok || d != "" {
		t.Fatalf("reverse lookup must fail when Fake-IP disabled, got (%q, %v)", d, ok)
	}
	if d, ok := r.LookupDomainByFakeIPStr("198.18.0.1"); ok || d != "" {
		t.Fatalf("string reverse lookup must fail when Fake-IP disabled, got (%q, %v)", d, ok)
	}
}

// 零配置时补全默认上游/超时/容量；存在但打不开的 GeoIP 路径（目录）只告警不致命。
func TestNewResolver_DefaultsAndUnreadableGeoIP(t *testing.T) {
	r, err := NewResolver(Config{GeoIPFile: t.TempDir()})
	if err != nil {
		t.Fatalf("NewResolver with empty config failed: %v", err)
	}
	defer r.Close()

	if len(r.cfg.DirectUpstreams) == 0 || len(r.cfg.RemoteUpstreams) == 0 {
		t.Fatal("default upstreams must be filled in")
	}
	if r.cfg.Timeout <= 0 || r.cfg.CacheCapacity <= 0 {
		t.Fatalf("default timeout/capacity must be positive: %+v", r.cfg)
	}
}

// ResolveAll 的失败路径：A 查询失败直接返回错误。
func TestResolver_ResolveAll_AFailure(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(*dns.Msg) *dns.Msg { return nil })
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.Timeout = 150 * time.Millisecond
	})

	if _, err := r.ResolveAll(context.Background(), "dead-a.test"); err == nil {
		t.Fatal("expected error when the A query fails")
	}
}

// A 返回空 Answer、AAAA 查询失败时，错误信息必须带出 AAAA 的失败原因。
func TestResolver_ResolveAll_EmptyAAndFailedAAAA(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		resp := new(dns.Msg)
		resp.SetReply(req)
		if req.Question[0].Qtype == dns.TypeAAAA {
			return nil // 模拟 AAAA 上游静默
		}
		return resp // A 返回合法但无 Answer
	})
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.Timeout = 150 * time.Millisecond
		// 命中直连白名单时空 Answer 响应会原样返回；竞速路径会丢弃它
		c.DirectDomains = []string{"empty-aaaa.test"}
	})

	_, err := r.ResolveAll(context.Background(), "empty-aaaa.test")
	if err == nil || !strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("expected error mentioning the aaaa failure, got %v", err)
	}
}

// Fake-IP 开启后，Exchange 对非直连域名直接合成 A 响应，并支持 PTR 反查。
func TestResolver_ExchangeFakeIPSynthetic(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, replyA("10.0.0.50"))
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.EnableFakeIP = true
		c.FakeIPRange = "198.18.0.0/15"
	})

	// 先分配一个 Fake-IP
	ips, err := r.Resolve(context.Background(), "synth.test")
	if err != nil {
		t.Fatalf("allocate fake ip failed: %v", err)
	}
	fakeIP := ips[0]

	// 直接走 Exchange 查 A，应得到合成应答且不触达上游
	aReq := upstream.BuildQuery("synth.test", dns.TypeA)
	aResp, err := r.Exchange(context.Background(), aReq)
	if err != nil {
		t.Fatalf("fake-ip A exchange failed: %v", err)
	}
	if len(aResp.Answer) != 1 {
		t.Fatalf("expected 1 synthetic A record, got %d", len(aResp.Answer))
	}
	aRR, ok := aResp.Answer[0].(*dns.A)
	if !ok || !aRR.A.Equal(fakeIP) {
		t.Fatalf("synthetic A record mismatch: %v vs %v", aRR, fakeIP)
	}

	// PTR 反查应返回原域名
	arpa := fakeIP.String()
	arpa = strings.Join(reverseOctets(arpa), ".") + ".in-addr.arpa"
	ptrReq := upstream.BuildQuery(arpa, dns.TypePTR)
	ptrResp, err := r.Exchange(context.Background(), ptrReq)
	if err != nil {
		t.Fatalf("fake-ip PTR exchange failed: %v", err)
	}
	if len(ptrResp.Answer) != 1 {
		t.Fatalf("expected 1 PTR record, got %d", len(ptrResp.Answer))
	}
	ptr, ok := ptrResp.Answer[0].(*dns.PTR)
	if !ok || ptr.Ptr != "synth.test." {
		t.Fatalf("unexpected PTR answer: %v", ptrResp.Answer)
	}
}

func reverseOctets(ip string) []string {
	parts := strings.Split(ip, ".")
	out := make([]string, 4)
	for i := range parts {
		out[3-i] = parts[i]
	}
	return out
}

// queryRemote 自身的"无远程上游回退直连"防御分支（exchangeInternal 已先行拦截，
// 这里白盒直调保证该分支自身可用）。
func TestResolver_QueryRemote_EmptyGuardDirectCall(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, replyA("10.0.0.60"))
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{":53"})

	req := upstream.BuildQuery("guard.test", dns.TypeA)
	resp, err := r.queryRemote(context.Background(), req)
	if err != nil {
		t.Fatalf("queryRemote empty guard should fall back to direct: %v", err)
	}
	if ips := upstream.ExtractIPs(resp); len(ips) != 1 || ips[0].String() != "10.0.0.60" {
		t.Fatalf("unexpected guard fallback answer: %v", ips)
	}
}

// 直连先失败、远程后成功时，竞速循环在收到第二个结果时立即采纳远程。
func TestResolver_QueryParallel_RemoteArrivesLast(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, servFailHandler)
	defer stopDirect()
	remoteAddr, _, stopRemote := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		time.Sleep(150 * time.Millisecond)
		return replyA("198.51.100.70")(req)
	})
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr})

	ips, err := r.Resolve(context.Background(), "remote-last.test")
	if err != nil {
		t.Fatalf("expected remote answer: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.70" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}

// 直连静默、远程延迟后成功：等不到直连结果时，超时分支也要带上已有的远程结果，
// 而不是干等到整个查询失败。
func TestResolver_QueryParallel_TimeoutReturnsRemote(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, func(*dns.Msg) *dns.Msg { return nil })
	defer stopDirect()
	remoteAddr, _, stopRemote := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		time.Sleep(150 * time.Millisecond)
		return replyA("198.51.100.80")(req)
	})
	defer stopRemote()

	r := newStubResolver(t, []string{directAddr}, []string{remoteAddr}, func(c *Config) {
		c.Timeout = 1 * time.Second
	})

	ips, err := r.Resolve(context.Background(), "timeout-remote.test")
	if err != nil {
		t.Fatalf("expected the remote answer on timeout path: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.80" {
		t.Fatalf("unexpected ips: %v", ips)
	}
}
