package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// newResp 构造一条带单个 A 记录的应答
func newResp(t *testing.T, name string, ttl uint32, ip string) *dns.Msg {
	t.Helper()
	rr, err := dns.NewRR(fmt.Sprintf("%s %d IN A %s", dns.Fqdn(name), ttl, ip))
	if err != nil {
		t.Fatalf("dns.NewRR failed: %v", err)
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	m.Answer = append(m.Answer, rr)
	return m
}

func TestKeyForMsg(t *testing.T) {
	// 大小写与末尾点都应归一化
	a := KeyForMsg("Example.COM.", dns.TypeA, dns.ClassINET)
	b := KeyForMsg("example.com", dns.TypeA, dns.ClassINET)
	if a != b {
		t.Fatalf("expected normalized keys to match, got %q and %q", a, b)
	}
	if KeyForMsg("example.com", dns.TypeAAAA, dns.ClassINET) == a {
		t.Fatalf("different qtype must produce different keys")
	}
	if KeyForMsg("example.com", dns.TypeA, dns.ClassCHAOS) == a {
		t.Fatalf("different qclass must produce different keys")
	}
}

func TestCacheSetAndGet(t *testing.T) {
	c := NewDNSCache(10)
	key := KeyForMsg("example.com", dns.TypeA, dns.ClassINET)

	if _, ok := c.Get(key); ok {
		t.Fatalf("expected cache miss before Set")
	}

	c.Set(key, newResp(t, "example.com", 300, "1.2.3.4"))
	got, ok := c.Get(key)
	if !ok {
		t.Fatalf("expected cache hit after Set")
	}
	if len(got.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(got.Answer))
	}

	// 返回的必须是副本，外部修改不能污染缓存
	got.Answer[0].Header().Ttl = 1
	again, _ := c.Get(key)
	if again.Answer[0].Header().Ttl == 1 {
		t.Fatalf("Get must return a deep copy")
	}
}

func TestCacheIgnoresEmptyAnswer(t *testing.T) {
	c := NewDNSCache(10)
	key := KeyForMsg("example.com", dns.TypeA, dns.ClassINET)

	c.Set(key, &dns.Msg{})
	if _, ok := c.Get(key); ok {
		t.Fatalf("msg without answers must not be cached")
	}
	c.Set(key, nil)
	if _, ok := c.Get(key); ok {
		t.Fatalf("nil msg must not be cached")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewDNSCache(10)
	key := KeyForMsg("example.com", dns.TypeA, dns.ClassINET)

	c.Set(key, newResp(t, "example.com", 300, "1.2.3.4"))
	// 直接把过期时间改到过去，避免测试真的等 300s
	c.mu.Lock()
	c.items[key].ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()

	if _, ok := c.Get(key); ok {
		t.Fatalf("expected expired entry to be a miss")
	}
	if c.Len() != 0 {
		t.Fatalf("expected expired entry to be evicted, len=%d", c.Len())
	}
}

// TestCacheDecaysTTLOnGet 命中缓存时返回的 TTL 必须是剩余存活时间，
// 否则下游会拿到一个"看起来还很新鲜"的过期时间戳。
func TestCacheDecaysTTLOnGet(t *testing.T) {
	c := NewDNSCache(10)
	key := KeyForMsg("example.com", dns.TypeA, dns.ClassINET)

	c.Set(key, newResp(t, "example.com", 300, "1.2.3.4"))
	c.mu.Lock()
	c.items[key].ExpiresAt = time.Now().Add(10 * time.Second)
	c.mu.Unlock()

	got, ok := c.Get(key)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	ttl := got.Answer[0].Header().Ttl
	if ttl > 10 {
		t.Fatalf("expected decayed ttl <= 10, got %d", ttl)
	}
	if ttl == 0 {
		t.Fatalf("expected decayed ttl > 0, got %d", ttl)
	}
}

func TestCacheCapacityEviction(t *testing.T) {
	c := NewDNSCache(2)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("example%d.com", i)
		c.Set(KeyForMsg(name, dns.TypeA, dns.ClassINET), newResp(t, name, 300, "1.2.3.4"))
	}
	if n := c.Len(); n > 2 {
		t.Fatalf("expected cache to respect capacity 2, got %d", n)
	}
}

func TestCachePurgeExpiredAndClear(t *testing.T) {
	c := NewDNSCache(10)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("example%d.com", i)
		key := KeyForMsg(name, dns.TypeA, dns.ClassINET)
		c.Set(key, newResp(t, name, 300, "1.2.3.4"))
		c.mu.Lock()
		c.items[key].ExpiresAt = time.Now().Add(-time.Second)
		c.mu.Unlock()
	}
	if n := c.PurgeExpired(); n != 3 {
		t.Fatalf("expected 3 purged, got %d", n)
	}
	if c.Len() != 0 {
		t.Fatalf("expected empty cache, got %d", c.Len())
	}

	c.Set(KeyForMsg("a.com", dns.TypeA, dns.ClassINET), newResp(t, "a.com", 300, "1.2.3.4"))
	c.Clear()
	if c.Len() != 0 {
		t.Fatalf("expected Clear to empty the cache, got %d", c.Len())
	}
}

func TestDoSingleFlightDedup(t *testing.T) {
	c := NewDNSCache(10)
	var calls int32

	var wg sync.WaitGroup
	results := make([]*dns.Msg, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = c.DoSingleFlight("shared", func() (*dns.Msg, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(20 * time.Millisecond)
				return newResp(t, "example.com", 300, "1.2.3.4"), nil
			})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected the query to be deduplicated to 1 call, got %d", got)
	}
	for i := range results {
		if errs[i] != nil || results[i] == nil {
			t.Fatalf("caller %d got err=%v resp=%v", i, errs[i], results[i])
		}
	}
}

// TestDoSingleFlightNilResult 保证 fn 返回 (nil, nil) 时不 panic
func TestDoSingleFlightNilResult(t *testing.T) {
	c := NewDNSCache(10)
	msg, err := c.DoSingleFlight("nil", func() (*dns.Msg, error) {
		return nil, nil
	})
	if msg != nil || err != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", msg, err)
	}
}

func TestDoSingleFlightPropagatesError(t *testing.T) {
	c := NewDNSCache(10)
	want := fmt.Errorf("boom")
	msg, err := c.DoSingleFlight("err", func() (*dns.Msg, error) {
		return nil, want
	})
	if err != want {
		t.Fatalf("expected error %v, got %v", want, err)
	}
	if msg != nil {
		t.Fatalf("expected nil msg, got %v", msg)
	}

	// 失败的 key 不应被记住，下一次仍然会真正执行 fn
	var calls int32
	_, _ = c.DoSingleFlight("err", func() (*dns.Msg, error) {
		atomic.AddInt32(&calls, 1)
		return nil, want
	})
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected fn to run again after a failure, ran %d times", got)
	}
}
