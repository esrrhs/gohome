package fakeip

import (
	"testing"
	"time"
)

func TestFakeIPPool(t *testing.T) {
	pool := NewFakeIPPool(Config{
		CIDR: "198.18.0.0/28", // 小型池便于测试
		TTL:  100 * time.Millisecond,
	})

	d1 := "google.com"
	ip1 := pool.Allocate(d1)
	if ip1 == nil {
		t.Fatalf("expected allocated IP, got nil")
	}

	if !IsFakeIP(ip1) {
		t.Fatalf("expected IsFakeIP true, got false")
	}

	// 再次分配相同域名，应得到相同 IP
	ip1Again := pool.Allocate(d1)
	if !ip1.Equal(ip1Again) {
		t.Fatalf("expected same IP for %s, got %v and %v", d1, ip1, ip1Again)
	}

	// 反查
	lookedUp, ok := pool.LookupDomainByIP(ip1)
	if !ok || lookedUp != d1 {
		t.Fatalf("expected %s, got %s (ok=%v)", d1, lookedUp, ok)
	}

	// 分配不同域名
	d2 := "github.com"
	ip2 := pool.Allocate(d2)
	if ip1.Equal(ip2) {
		t.Fatalf("expected different IPs for different domains")
	}

	lookedUp2, ok2 := pool.LookupDomainByIPStr(ip2.String())
	if !ok2 || lookedUp2 != d2 {
		t.Fatalf("expected %s, got %s (ok=%v)", d2, lookedUp2, ok2)
	}

	// 测试过期
	time.Sleep(150 * time.Millisecond)
	_, okAfterExpired := pool.LookupDomainByIP(ip1)
	if okAfterExpired {
		t.Fatalf("expected lookup to fail after expiration")
	}
}
