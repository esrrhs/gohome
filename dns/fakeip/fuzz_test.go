package fakeip

import (
	"testing"
)

// FuzzFakeIPRoundTrip 检查 Fake-IP 池的核心契约：分配出去的 IP 必须能反查回
// 同一个域名，同一域名重复分配必须复用同一个 IP，且分配的 IP 必须落在池的
// CIDR 内并被 IsFakeIP 识别。域名来自 DNS 查询，任意字节都不得 panic。
func FuzzFakeIPRoundTrip(f *testing.F) {
	f.Add("example.com")
	f.Add("www.example.com")
	f.Add("")
	f.Add(".")
	f.Add("EXAMPLE.COM.")
	f.Add("..a..b..")
	f.Add("xn--80ak6aa92e.com")

	f.Fuzz(func(t *testing.T, domain string) {
		p := NewFakeIPPool()

		ip := p.Allocate(domain)
		if ip == nil {
			// 池耗尽或域名被拒绝时不分配，属预期
			return
		}

		// 1. 反查必须命中，且得到规范化后的域名
		want := NormalizeDomain(domain)
		if got, ok := p.LookupDomainByIP(ip); !ok {
			t.Fatalf("Allocate(%q) -> %v, but LookupDomainByIP missed", domain, ip)
		} else if got != want {
			t.Fatalf("reverse lookup got %q, want %q", got, want)
		}

		// 2. 字符串入口与 net.IP 入口必须一致
		if got, ok := p.LookupDomainByIPStr(ip.String()); !ok || got != want {
			t.Fatalf("LookupDomainByIPStr(%q) = (%q, %v), want (%q, true)", ip, got, ok, want)
		}

		// 3. 同一域名重复分配必须复用同一个 IP
		if again := p.Allocate(domain); again == nil || !again.Equal(ip) {
			t.Fatalf("re-Allocate(%q) returned %v, want %v", domain, again, ip)
		}

		// 4. 分配出的 IP 必须被识别为 Fake-IP
		if !p.IsFakeIP(ip) {
			t.Fatalf("IsFakeIP(%v) must be true for an allocated IP", ip)
		}
		if !IsFakeIP(ip) {
			t.Fatalf("package-level IsFakeIP(%v) must be true", ip)
		}
		if !IsFakeIPStr(ip.String()) {
			t.Fatalf("IsFakeIPStr(%q) must be true", ip.String())
		}
	})
}

// FuzzNormalizeDomain 检查域名规范化：幂等、大小写不敏感、去掉首尾点。
func FuzzNormalizeDomain(f *testing.F) {
	f.Add("example.com")
	f.Add("")
	f.Add("...")
	f.Add("MIXED.Case.COM")

	f.Fuzz(func(t *testing.T, domain string) {
		once := NormalizeDomain(domain)
		// 幂等：再规范化一次不再变化
		if twice := NormalizeDomain(once); twice != once {
			t.Fatalf("NormalizeDomain is not idempotent: %q -> %q -> %q", domain, once, twice)
		}
		// 与小写输入一致（大小写不敏感）
		if lower := NormalizeDomain(lowerASCII(domain)); lower != once {
			t.Fatalf("NormalizeDomain is case sensitive: %q vs %q", once, lower)
		}
		// 结果不含首尾点
		if len(once) > 0 && (once[0] == '.' || once[len(once)-1] == '.') {
			t.Fatalf("NormalizeDomain(%q) left a leading/trailing dot: %q", domain, once)
		}
	})
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
