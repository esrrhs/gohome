package fakeip

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestHardenFakeIPTinyCIDR /31 与 /32 极小网段：分配结果必须落在声明网段内。
// 旧实现 /31 时 maxOffset 保留默认 131070，分配全部越界且 IsFakeIP=false。
func TestHardenFakeIPTinyCIDR(t *testing.T) {
	// /31：去网络/广播后 0 个常规可用地址，maxOffset 兜底为 1，只复用 .1
	p := NewFakeIPPool(Config{CIDR: "10.77.0.0/31", TTL: time.Hour})
	if p.maxOffset != 1 {
		t.Fatalf("/31 maxOffset = %d, want 1", p.maxOffset)
	}
	for i := 0; i < 3; i++ {
		ip := p.Allocate("d" + string(rune('a'+i)) + ".example.com")
		if !p.ipnet.Contains(ip) {
			t.Fatalf("alloc %v outside /31", ip)
		}
		if !p.IsFakeIP(ip) {
			t.Fatalf("IsFakeIP(%v) must follow configured /31", ip)
		}
		if ip.String() != "10.77.0.1" {
			t.Fatalf("/31 pool must reuse 10.77.0.1, got %v", ip)
		}
	}

	// /32：唯一地址本身可用
	p32 := NewFakeIPPool(Config{CIDR: "10.88.0.5/32", TTL: time.Hour})
	want32 := uint32(10)<<24 | uint32(88)<<16 | 5
	if p32.maxOffset != 1 || p32.baseIP != want32 {
		t.Fatalf("/32 pool base=%d maxOffset=%d", p32.baseIP, p32.maxOffset)
	}
	ip := p32.Allocate("single.example.com")
	if ip.String() != "10.88.0.5" || !p32.IsFakeIP(ip) {
		t.Fatalf("/32 alloc = %v", ip)
	}
	if d, ok := p32.LookupDomainByIP(ip); !ok || d != "single.example.com" {
		t.Fatalf("/32 reverse lookup = %q, %v", d, ok)
	}
}

// TestHardenFakeIPZeroValueFallback 未构造的零值池：IsFakeIP 退回包级默认网段。
func TestHardenFakeIPZeroValueFallback(t *testing.T) {
	var p FakeIPPool
	if p.ipnet != nil {
		t.Fatal("zero-value pool must have nil ipnet")
	}
	if !p.IsFakeIP(net.ParseIP("198.18.0.1")) {
		t.Error("zero-value pool must fall back to default range")
	}
	if p.IsFakeIP(net.ParseIP("8.8.8.8")) || p.IsFakeIP(nil) {
		t.Error("out-of-range and nil must be false")
	}
	if p.IsFakeIP(net.ParseIP("2001:db8::1")) {
		t.Error("ipv6 must be false")
	}
}

// TestHardenFakeIPForceRecycle 地址池占满后走强制回收分支：
// 游标位置的旧映射被顶掉，双向映射总数不增。
// 注意游标先 +1 再取偏移：/30 的前两个分配依次是 .2、.1。
func TestHardenFakeIPForceRecycle(t *testing.T) {
	p := NewFakeIPPool(Config{CIDR: "10.9.0.0/30", TTL: time.Hour})
	ipA := p.Allocate("a.example.com") // current=1 → .2
	ipB := p.Allocate("b.example.com") // current=0 → .1
	if ipA.String() != "10.9.0.2" || ipB.String() != "10.9.0.1" {
		t.Fatalf("unexpected initial ips: %v %v", ipA, ipB)
	}
	if p.Len() != 2 {
		t.Fatalf("len = %d, want 2", p.Len())
	}

	// 池满：扫描两轮都撞有效占用，强制回收游标指向的 .2（属于 a）
	ipC := p.Allocate("c.example.com")
	if ipC.String() != "10.9.0.2" {
		t.Fatalf("force recycle must take cursor IP .2, got %v", ipC)
	}
	if p.Len() != 2 {
		t.Fatalf("forced recycle must not grow pool, len=%d", p.Len())
	}
	// 强制回收后 a 的域名映射必须消失，.2 的反查指向新主人 c
	p.mu.RLock()
	_, aGone := p.domainToIP["a.example.com"]
	cur := p.ipToDomain[ipA.String()]
	p.mu.RUnlock()
	if aGone {
		t.Fatal("evicted domain a mapping must be deleted")
	}
	if cur == nil || cur.domain != "c.example.com" {
		t.Fatalf(".2 owner = %v, want c.example.com", cur)
	}
	if d, ok := p.LookupDomainByIP(ipC); !ok || d != "c.example.com" {
		t.Fatalf("new owner of .2 = %q, %v", d, ok)
	}
	if d, ok := p.LookupDomainByIP(ipB); !ok || d != "b.example.com" {
		t.Fatalf("unrelated .1 mapping must survive, got %q %v", d, ok)
	}
}

// TestHardenFakeIPExpiredDomain 同域名映射过期后重新分配：旧双向映射被清，
// 新映射与反查一致。
func TestHardenFakeIPExpiredDomain(t *testing.T) {
	p := NewFakeIPPool(Config{CIDR: "10.10.0.0/29", TTL: 20 * time.Millisecond})
	old := p.Allocate("old.example.com") // cur1 → .2
	if _, ok := p.LookupDomainByIP(old); !ok {
		t.Fatal("fresh mapping must resolve")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := p.LookupDomainByIP(old); ok {
		t.Fatal("expired mapping must not resolve")
	}

	rep := p.Allocate("old.example.com") // 过期旧映射删除，游标前进到 .3
	if rep.String() != "10.10.0.3" {
		t.Fatalf("re-allocate must advance to .3, got %v", rep)
	}
	if p.Len() != 1 {
		t.Fatalf("re-allocate after expiry must not duplicate, len=%d", p.Len())
	}
	if _, ok := p.LookupDomainByIPStr(old.String()); ok {
		t.Fatal("old IP slot owner must be gone")
	}
	if d, ok := p.LookupDomainByIP(rep); !ok || d != "old.example.com" {
		t.Fatalf("new slot reverse lookup = %q, %v", d, ok)
	}
}

// TestHardenFakeIPExpiredIPSlot 游标扫到过期的 IP 侧条目（未满容触发不到
// purge 的防御路径）：必须清掉陈旧双向映射并复用该槽位。
func TestHardenFakeIPExpiredIPSlot(t *testing.T) {
	p := NewFakeIPPool(Config{CIDR: "10.10.0.0/29", TTL: time.Hour})

	// 白箱布置：下次分配(cur1)将落到 .2，该槽已有一份过期条目
	ghostIP := net.IPv4(10, 10, 0, 2).To4()
	old := &entry{
		domain:   "ghost.example.com",
		ip:       ghostIP,
		expireAt: time.Now().Add(-time.Hour),
	}
	p.mu.Lock()
	p.domainToIP["ghost.example.com"] = old
	p.ipToDomain[ghostIP.String()] = old
	p.current = 0 // 下一步 cur=(0+1)%6=1 → base+1=.2
	p.mu.Unlock()

	ip := p.Allocate("new.example.com")
	if ip.String() != "10.10.0.2" {
		t.Fatalf("expired slot must be reused, got %v", ip)
	}
	p.mu.RLock()
	_, ghostDomain := p.domainToIP["ghost.example.com"]
	cur := p.ipToDomain[ghostIP.String()]
	p.mu.RUnlock()
	if ghostDomain {
		t.Fatal("stale ghost domain mapping must be purged")
	}
	if cur == nil || cur.domain != "new.example.com" {
		t.Fatalf("slot must be owned by new.example.com, got %v", cur)
	}
	if d, ok := p.LookupDomainByIP(ip); !ok || d != "new.example.com" {
		t.Fatalf("new owner reverse lookup = %q, %v", d, ok)
	}
}

// TestHardenFakeIPNormalizeAndConcurrent 归一化与并发安全冒烟。
func TestHardenFakeIPNormalizeAndConcurrent(t *testing.T) {
	if got := NormalizeDomain("WWW.Example.COM."); got != "www.example.com" {
		t.Fatalf("NormalizeDomain = %q", got)
	}
	p := NewFakeIPPool(Config{CIDR: "10.20.0.0/24", TTL: time.Hour})

	// 大小写/首尾点归一后必须复用同一映射
	a := p.Allocate("FOO.example.com")
	b := p.Allocate(".foo.example.com.")
	if a.String() != b.String() || p.Len() != 1 {
		t.Fatalf("normalized domain must share one mapping: %v %v len=%d", a, b, p.Len())
	}
	if d, ok := p.LookupDomainByIPStr(a.String()); !ok || d != "foo.example.com" {
		t.Fatalf("reverse = %q, %v", d, ok)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				d := "x" + itoaH(i*4+seed) + ".example.com"
				ip := p.Allocate(d)
				if !p.IsFakeIP(ip) {
					t.Errorf("allocated %v outside pool", ip)
					return
				}
				_ = p.Len()
				_, _ = p.LookupDomainByIP(ip)
			}
		}(g)
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func itoaH(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
