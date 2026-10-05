package fakeip

import (
	"net"
	"testing"
	"time"
)

// TestFakeIPPoolCustomCIDR 自定义网段时，判定必须跟随配置而不是写死的 198.18/198.19
func TestFakeIPPoolCustomCIDR(t *testing.T) {
	const custom = "10.66.0.0/16"
	pool := NewFakeIPPool(Config{CIDR: custom, TTL: time.Hour})

	if pool.CIDR() != custom {
		t.Fatalf("CIDR() = %q, want %q", pool.CIDR(), custom)
	}

	ip := pool.Allocate("example.com")
	if ip == nil {
		t.Fatalf("expected allocated IP")
	}
	if !pool.IsFakeIP(ip) {
		t.Fatalf("pool.IsFakeIP(%v) = false, want true", ip)
	}
	// 包级函数只认默认网段，这里应当为 false —— 正是这个差异导致自定义网段失效
	if IsFakeIP(ip) {
		t.Fatalf("package-level IsFakeIP should not match a custom range IP %v", ip)
	}
	if pool.IsFakeIP(net.ParseIP("198.18.0.1")) {
		t.Fatalf("custom range pool must not claim the default range")
	}
	if !IsFakeIP(net.ParseIP("198.18.0.1")) {
		t.Fatalf("package-level IsFakeIP should match the default range")
	}
}

func TestFakeIPPoolInvalidCIDRFallsBack(t *testing.T) {
	for _, bad := range []string{"", "not-a-cidr", "2001:db8::/32", "10.0.0.0/33"} {
		pool := NewFakeIPPool(Config{CIDR: bad})
		if pool.CIDR() != defaultFakeIPCIDR {
			t.Fatalf("CIDR %q: expected fallback to %q, got %q", bad, defaultFakeIPCIDR, pool.CIDR())
		}
		if pool.maxOffset == 0 {
			t.Fatalf("CIDR %q: maxOffset must never be 0 (used as a modulus)", bad)
		}
		// 极端网段也不能因为除零或越界而崩
		ip := pool.Allocate("example.com")
		if ip == nil {
			t.Fatalf("CIDR %q: expected allocated IP", bad)
		}
	}
}

func TestFakeIPPoolWrapAround(t *testing.T) {
	// /30 只有 4 个地址，maxOffset = 2，足以触发环绕回收
	pool := NewFakeIPPool(Config{CIDR: "198.18.0.0/30", TTL: time.Hour})

	seen := make(map[string]string)
	for i := 0; i < 6; i++ {
		d := string(rune('a'+i)) + ".example.com"
		ip := pool.Allocate(d)
		if ip == nil {
			t.Fatalf("domain %s: expected allocated IP", d)
		}
		if !pool.IsFakeIP(ip) {
			t.Fatalf("domain %s: allocated IP %v outside the pool range", d, ip)
		}
		seen[ip.String()] = d
	}
	// 环绕后旧映射必须被回收，否则 ipToDomain/domainToIP 会一直膨胀
	if pool.Len() > int(pool.maxOffset) {
		t.Fatalf("expected at most %d live mappings, got %d", pool.maxOffset, pool.Len())
	}
}

func TestFakeIPPoolPurgeExpired(t *testing.T) {
	pool := NewFakeIPPool(Config{CIDR: "198.18.0.0/24", TTL: 20 * time.Millisecond})

	pool.Allocate("a.example.com")
	pool.Allocate("b.example.com")
	if pool.Len() != 2 {
		t.Fatalf("expected 2 mappings, got %d", pool.Len())
	}

	time.Sleep(50 * time.Millisecond)
	if n := pool.PurgeExpired(); n != 2 {
		t.Fatalf("expected 2 purged, got %d", n)
	}
	if pool.Len() != 0 {
		t.Fatalf("expected empty pool after purge, got %d", pool.Len())
	}
	ip := pool.Allocate("c.example.com")
	if _, ok := pool.LookupDomainByIP(ip); !ok {
		t.Fatalf("expected reverse lookup to work after purge")
	}
}

func TestFakeIPPoolLookupEdgeCases(t *testing.T) {
	pool := NewFakeIPPool(Config{TTL: time.Hour})

	if _, ok := pool.LookupDomainByIP(nil); ok {
		t.Fatalf("nil IP must not resolve")
	}
	if _, ok := pool.LookupDomainByIP(net.ParseIP("2001:db8::1")); ok {
		t.Fatalf("IPv6 must not resolve in an IPv4 pool")
	}
	if _, ok := pool.LookupDomainByIPStr("not-an-ip"); ok {
		t.Fatalf("garbage IP string must not resolve")
	}
	if IsFakeIP(nil) {
		t.Fatalf("IsFakeIP(nil) must be false")
	}
	if IsFakeIP(net.ParseIP("2001:db8::1")) {
		t.Fatalf("IsFakeIP(ipv6) must be false")
	}
	if !IsFakeIPStr("198.19.255.254") || IsFakeIPStr("198.20.0.1") {
		t.Fatalf("IsFakeIPStr range check is wrong")
	}
}
