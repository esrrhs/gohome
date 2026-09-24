package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestResolver_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableFakeIP = false // 验证真实解析模式

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("failed to create resolver: %v", err)
	}

	// 1. 直连顶级域/域名命中判定
	if r.directDomains.Has("baidu.com") != true {
		t.Errorf("expected baidu.com to be direct")
	}
	if r.directDomains.Has("gov.cn") != true {
		t.Errorf("expected gov.cn to be direct")
	}

	// 2. 直连 CIDR 判定
	if r.directIPs.Contains(net.ParseIP("192.168.1.1")) != true {
		t.Errorf("expected 192.168.1.1 to be direct IP")
	}
	if r.directIPs.Contains(net.ParseIP("8.8.8.8")) == true {
		t.Errorf("expected 8.8.8.8 not to be direct IP")
	}

	// 3. 动态热更新测试
	// 动态添加一个私有 CIDR
	r.UpdateDirectIPs([]string{"1.2.3.4/32"})
	if !r.directIPs.Contains(net.ParseIP("1.2.3.4")) {
		t.Errorf("expected updated direct IP 1.2.3.4 to match")
	}

	// 动态添加直连域名
	r.UpdateDirectDomains([]string{"custom-internal.corp"})
	if !r.directDomains.Has("api.custom-internal.corp") {
		t.Errorf("expected custom-internal.corp to match")
	}
}

func TestResolver_FakeIPMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableFakeIP = true // 开启 Fake-IP 模式
	cfg.DirectDomains = []string{"direct.internal"}

	r, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("failed to create resolver: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. 非直连域名：应立即分配 Fake-IP
	ips, err := r.Resolve(ctx, "google.com")
	if err != nil {
		t.Fatalf("failed to resolve google.com in fake-ip mode: %v", err)
	}
	if len(ips) == 0 {
		t.Fatalf("no ips returned")
	}

	fakeIP := ips[0]
	if !r.IsFakeIP(fakeIP) {
		t.Fatalf("expected fake IP, got %v", fakeIP)
	}

	// 2. Fake-IP 反查原始域名
	originDomain, ok := r.LookupDomainByFakeIP(fakeIP)
	if !ok || originDomain != "google.com" {
		t.Fatalf("expected google.com, got %s (ok=%v)", originDomain, ok)
	}

	// 3. DNS PTR 查询测试
	ptrReq := new(dns.Msg)
	arpaName, err := dns.ReverseAddr(fakeIP.String())
	if err != nil {
		t.Fatalf("ReverseAddr failed: %v", err)
	}
	ptrReq.SetQuestion(arpaName, dns.TypePTR)

	ptrResp, err := r.Exchange(ctx, ptrReq)
	if err != nil {
		t.Fatalf("PTR query failed: %v", err)
	}
	if len(ptrResp.Answer) == 0 {
		t.Fatalf("no PTR answer returned")
	}
	if ptr, ok := ptrResp.Answer[0].(*dns.PTR); ok {
		if ptr.Ptr != "google.com." {
			t.Fatalf("expected google.com., got %s", ptr.Ptr)
		}
	} else {
		t.Fatalf("expected PTR record")
	}
}
