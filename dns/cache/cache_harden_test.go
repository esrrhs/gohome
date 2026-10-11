package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// ---------- NewDNSCache ----------

func TestHardenNewDNSCacheDefaultCapacity(t *testing.T) {
	for _, n := range []int{0, -1} {
		c := NewDNSCache(n)
		if c.capacity != 2000 {
			t.Fatalf("NewDNSCache(%d) capacity = %d, want 2000", n, c.capacity)
		}
	}
	c := NewDNSCache(0)
	for i := 0; i < 2001; i++ {
		name := fmt.Sprintf("host%d.com", i)
		c.Set(KeyForMsg(name, dns.TypeA, dns.ClassINET), newResp(t, name, 300, "1.1.1.1"))
	}
	if c.Len() != 2000 {
		t.Fatalf("default capacity must cap entries at 2000, got %d", c.Len())
	}
	last := KeyForMsg("host2000.com", dns.TypeA, dns.ClassINET)
	if _, ok := c.Get(last); !ok {
		t.Fatal("the 2001st entry must be present after eviction")
	}
}

// ---------- Set TTL 收敛 ----------

func entryExpiry(c *DNSCache, key string) time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[key].ExpiresAt
}

func TestHardenSetTTLClamps(t *testing.T) {
	c := NewDNSCache(100)
	before := time.Now()

	// TTL=0 被保底到 5s
	key0 := KeyForMsg("zero.com", dns.TypeA, dns.ClassINET)
	c.Set(key0, newResp(t, "zero.com", 0, "1.1.1.1"))
	d0 := entryExpiry(c, key0).Sub(before)
	if d0 < 4*time.Second || d0 > 7*time.Second {
		t.Fatalf("ttl=0 clamp to ~5s, got expiry delta %v", d0)
	}

	// 超大 TTL 被截到 86400s
	keyMax := KeyForMsg("big.com", dns.TypeA, dns.ClassINET)
	c.Set(keyMax, newResp(t, "big.com", 999999, "1.1.1.1"))
	d1 := entryExpiry(c, keyMax).Sub(before)
	if d1 < 86399*time.Second || d1 > 86401*time.Second {
		t.Fatalf("ttl=999999 clamp to 86400s, got %v", d1)
	}

	// 多条 Answer 取最小 TTL
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn("multi.com"), dns.TypeA)
	for _, ttl := range []uint32{100, 30, 200} {
		rr, err := dns.NewRR(fmt.Sprintf("%s %d IN A 1.2.3.4", dns.Fqdn("multi.com"), ttl))
		if err != nil {
			t.Fatal(err)
		}
		m.Answer = append(m.Answer, rr)
	}
	keyMulti := KeyForMsg("multi.com", dns.TypeA, dns.ClassINET)
	c.Set(keyMulti, m)
	d2 := entryExpiry(c, keyMulti).Sub(before)
	if d2 < 29*time.Second || d2 > 31*time.Second {
		t.Fatalf("min ttl must be 30s, got %v", d2)
	}
}

// ---------- Set 容量驱逐 ----------

func TestHardenEvictionPrefersExpired(t *testing.T) {
	c := NewDNSCache(2)
	ka := KeyForMsg("a.com", dns.TypeA, dns.ClassINET)
	kb := KeyForMsg("b.com", dns.TypeA, dns.ClassINET)
	c.Set(ka, newResp(t, "a.com", 300, "1.1.1.1"))
	c.Set(kb, newResp(t, "b.com", 300, "1.1.1.1"))

	// 让 b 过期：满容写入 c 时必须优先驱逐过期的 b，保留新鲜的 a
	c.mu.Lock()
	c.items[kb].ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()

	kc := KeyForMsg("c.com", dns.TypeA, dns.ClassINET)
	c.Set(kc, newResp(t, "c.com", 300, "1.1.1.1"))
	if c.Len() != 2 {
		t.Fatalf("len must stay at capacity, got %d", c.Len())
	}
	if _, ok := c.Get(ka); !ok {
		t.Fatal("fresh entry a must survive when an expired victim exists")
	}
	if _, ok := c.Get(kc); !ok {
		t.Fatal("new entry c must be cached")
	}
}

// TestHardenRefreshExistingKeyDoesNotEvict 容量已满时刷新同名 key，
// 不应误删任何其他条目（旧实现总会随手驱逐一个邻居）。
func TestHardenRefreshExistingKeyDoesNotEvict(t *testing.T) {
	c := NewDNSCache(2)
	ka := KeyForMsg("a.com", dns.TypeA, dns.ClassINET)
	kb := KeyForMsg("b.com", dns.TypeA, dns.ClassINET)
	c.Set(ka, newResp(t, "a.com", 300, "1.1.1.1"))
	c.Set(kb, newResp(t, "b.com", 300, "1.1.1.1"))

	for i := 0; i < 20; i++ {
		c.Set(ka, newResp(t, "a.com", 300, fmt.Sprintf("1.0.0.%d", i)))
		if c.Len() != 2 {
			t.Fatalf("refresh must keep len=2, got %d at iter %d", c.Len(), i)
		}
		if _, ok := c.Get(kb); !ok {
			t.Fatalf("unrelated entry b evicted by same-key refresh at iter %d", i)
		}
	}
}

// ---------- decayTTL ----------

func TestHardenDecayTTLDirect(t *testing.T) {
	// nil / 已到期不处理
	decayTTL(nil, 10*time.Second)
	m := newResp(t, "x.com", 300, "1.1.1.1")
	decayTTL(m, 0)
	if m.Answer[0].Header().Ttl != 300 {
		t.Fatalf("remaining<=0 must leave TTL untouched, got %d", m.Answer[0].Header().Ttl)
	}

	// 不足 1 秒时按 1 秒收敛
	m = newResp(t, "x.com", 300, "1.1.1.1")
	decayTTL(m, 500*time.Millisecond)
	if m.Answer[0].Header().Ttl != 1 {
		t.Fatalf("sub-second remaining must clamp to 1, got %d", m.Answer[0].Header().Ttl)
	}

	// Answer/Ns/Extra 三个区段都收敛，小于 remaining 的保持原值
	full := new(dns.Msg)
	full.SetQuestion(dns.Fqdn("full.com"), dns.TypeA)
	rrA, _ := dns.NewRR(fmt.Sprintf("%s 300 IN A 1.2.3.4", dns.Fqdn("full.com")))
	rrNs, _ := dns.NewRR(fmt.Sprintf("%s 300 IN NS ns.full.com.", dns.Fqdn("full.com")))
	rrSmall, _ := dns.NewRR(fmt.Sprintf("small.com. 5 IN A 2.3.4.5"))
	full.Answer = append(full.Answer, rrA)
	full.Ns = append(full.Ns, rrNs)
	full.Extra = append(full.Extra, rrSmall)
	decayTTL(full, 50*time.Second)
	if full.Answer[0].Header().Ttl != 50 {
		t.Fatalf("answer ttl = %d, want 50", full.Answer[0].Header().Ttl)
	}
	if full.Ns[0].Header().Ttl != 50 {
		t.Fatalf("ns ttl = %d, want 50", full.Ns[0].Header().Ttl)
	}
	if full.Extra[0].Header().Ttl != 5 {
		t.Fatalf("ttl below remaining must stay 5, got %d", full.Extra[0].Header().Ttl)
	}
}

// TestHardenDecayTTLPreservesOPT EDNS0 OPT 是元记录，其 Ttl 字段编码
// extended rcode/version/flags（DO 位为 0x8000），绝不能被 TTL 收敛改写。
func TestHardenDecayTTLPreservesOPT(t *testing.T) {
	m := newResp(t, "dnssec.com", 300, "1.2.3.4")
	opt := &dns.OPT{
		Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT, Class: 4096},
	}
	opt.SetDo()
	if opt.Hdr.Ttl != 0x8000 {
		t.Fatalf("sanity: DO bit must encode 0x8000, got %#x", opt.Hdr.Ttl)
	}
	m.Extra = append(m.Extra, opt)

	c := NewDNSCache(10)
	key := KeyForMsg("dnssec.com", dns.TypeA, dns.ClassINET)
	c.Set(key, m)
	c.mu.Lock()
	c.items[key].ExpiresAt = time.Now().Add(10 * time.Second)
	c.mu.Unlock()

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("cache hit expected")
	}
	var optAfter *dns.OPT
	for _, rr := range got.Extra {
		if o, isOpt := rr.(*dns.OPT); isOpt {
			optAfter = o
		}
	}
	if optAfter == nil {
		t.Fatal("OPT record must survive cache Get")
	}
	if optAfter.Hdr.Ttl != 0x8000 {
		t.Fatalf("OPT pseudo-TTL (DO flag) corrupted by TTL decay: got %#x, want 0x8000", optAfter.Hdr.Ttl)
	}
	if got.Answer[0].Header().Ttl > 10 {
		t.Fatalf("real TTL should still be decayed, got %d", got.Answer[0].Header().Ttl)
	}
}

// ---------- Get 边界 ----------

func TestHardenGetSubSecondRemaining(t *testing.T) {
	c := NewDNSCache(10)
	key := KeyForMsg("x.com", dns.TypeA, dns.ClassINET)
	c.Set(key, newResp(t, "x.com", 300, "1.1.1.1"))
	// 剩余不足 1 秒：命中且 TTL 收敛为 1（不会给出 0 或 300）
	c.mu.Lock()
	c.items[key].ExpiresAt = time.Now().Add(500 * time.Millisecond)
	c.mu.Unlock()
	got, ok := c.Get(key)
	if !ok {
		t.Fatal("entry with 500ms remaining must still be a hit")
	}
	if got.Answer[0].Header().Ttl != 1 {
		t.Fatalf("sub-second remaining TTL = %d, want 1", got.Answer[0].Header().Ttl)
	}
}

// ---------- DoSingleFlight 顺序与并发 ----------

func TestHardenSingleFlightDifferentKeys(t *testing.T) {
	c := NewDNSCache(10)
	var calls int32
	var wg sync.WaitGroup
	for _, k := range []string{"k1", "k2", "k3"} {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(k string) {
				defer wg.Done()
				_, _ = c.DoSingleFlight(k, func() (*dns.Msg, error) {
					atomic.AddInt32(&calls, 1)
					time.Sleep(10 * time.Millisecond)
					return newResp(t, "x.com", 60, "1.1.1.1"), nil
				})
			}(k)
		}
	}
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("different keys must not be deduplicated, got %d calls, want 3", got)
	}
}

func TestHardenCacheConcurrentStress(t *testing.T) {
	c := NewDNSCache(64)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				name := fmt.Sprintf("h%d-%d.com", g%4, i%128)
				key := KeyForMsg(name, dns.TypeA, dns.ClassINET)
				if i%3 == 0 {
					c.Set(key, newResp(t, name, uint32(1+i%300), "1.2.3.4"))
				} else {
					c.Get(key)
				}
				if i%50 == 0 {
					c.PurgeExpired()
					_ = c.Len()
				}
				if i%200 == 0 {
					c.Clear()
				}
				_, _ = c.DoSingleFlight(key, func() (*dns.Msg, error) {
					return newResp(t, name, 30, "1.2.3.4"), nil
				})
			}
		}(g)
	}
	wg.Wait()
}
