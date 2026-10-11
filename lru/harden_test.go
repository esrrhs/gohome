package lru

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHardenLRUZeroCapacity capacity<=0 必须被夹到 1：
// 旧实现下 Len()==0 恒等于 capacity 0，evict 空转，缓存无界增长。
func TestHardenLRUZeroCapacity(t *testing.T) {
	for _, cap := range []int{0, -1, -100} {
		c := NewLRUCache[int, int](cap, time.Minute)
		for i := 0; i < 100; i++ {
			c.Set(i, i)
		}
		if c.Size() != 1 {
			t.Fatalf("capacity=%d: size=%d, want bounded 1", cap, c.Size())
		}
		if v, ok := c.Get(99); !ok || v != 99 {
			t.Fatalf("capacity=%d: latest entry must be served, got %d,%v", cap, v, ok)
		}
		if _, ok := c.Get(0); ok {
			t.Fatalf("capacity=%d: oldest must be evicted", cap)
		}
	}
}

// TestHardenLRUMultiBadNumCaches numCaches<=0 不能在取模分片时除零 panic。
func TestHardenLRUMultiBadNumCaches(t *testing.T) {
	for _, n := range []int{0, -1} {
		c := NewLRUMultiCache[int, int](n, 10, time.Minute)
		if len(c.caches) != 1 {
			t.Fatalf("numCaches=%d: want 1 shard, got %d", n, len(c.caches))
		}
		c.Set(1, 11)
		if v, ok := c.Get(1); !ok || v != 11 {
			t.Fatalf("numCaches=%d: roundtrip failed: %d,%v", n, v, ok)
		}
		c.Clear()
		if c.Size() != 0 {
			t.Fatalf("numCaches=%d: clear failed, size=%d", n, c.Size())
		}
	}
}

// TestHardenLRUTTLRefresh 覆盖更新既有 key 必须刷新 TTL；过期 Get 返回零值惰性删除。
func TestHardenLRUTTLRefresh(t *testing.T) {
	c := NewLRUCache[string, string](3, 60*time.Millisecond)
	c.Set("k", "v1")
	time.Sleep(40 * time.Millisecond)
	c.Set("k", "v2") // 更新同时刷新 timestamp
	time.Sleep(40 * time.Millisecond)
	if v, ok := c.Get("k"); !ok || v != "v2" {
		t.Fatalf("refreshed entry must survive, got %q,%v", v, ok)
	}

	time.Sleep(70 * time.Millisecond)
	if v, ok := c.Get("k"); ok {
		t.Fatalf("expired entry must be removed, got %q", v)
	}
	if c.Size() != 0 {
		t.Fatalf("expired Get must lazily delete, size=%d", c.Size())
	}
}

// TestHardenResourceFetchError 拉取失败：返回零值与错误、计 miss、不入缓存，
// 后续请求必须重新拉取而不是缓存错误。
func TestHardenResourceFetchError(t *testing.T) {
	var calls atomic.Int32
	boom := errors.New("upstream boom")
	cache := NewLRUResourceCache[string, string](10, time.Minute, func(k string) (string, error) {
		calls.Add(1)
		return "", boom
	})

	v, err := cache.GetResource("k")
	if !errors.Is(err, boom) || v != "" {
		t.Fatalf("got %q,%v, want empty + boom", v, err)
	}
	if cache.Size() != 0 {
		t.Fatalf("failed fetch must not populate cache, size=%d", cache.Size())
	}
	if cache.CacheMiss() != 1 || cache.CacheHit() != 0 {
		t.Fatalf("miss=%d hit=%d, want 1/0", cache.CacheMiss(), cache.CacheHit())
	}

	if _, err = cache.GetResource("k"); !errors.Is(err, boom) {
		t.Fatalf("retry must re-fetch and fail, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("error must not be cached, fetch calls=%d", calls.Load())
	}
}

// TestHardenResourceFetchErrorCoalesce 同 key 并发拉取：leader 失败时，
// 所有等待者经 ReqQueue 复用同一结果拿到相同错误。
func TestHardenResourceFetchErrorCoalesce(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{})
	boom := errors.New("joint failure")
	cache := NewLRUResourceCache[string, int](10, time.Minute, func(k string) (int, error) {
		if calls.Add(1) == 1 {
			<-gate // leader 阻塞，等其他 goroutine 全部挂上等待
		}
		return 0, boom
	})

	const waiters = 7
	var wg sync.WaitGroup
	errs := make([]error, waiters)
	vs := make([]int, waiters)
	start := make(chan struct{})
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			vs[idx], errs[idx] = cache.GetResource("same")
		}(i)
	}
	close(start)
	// 等待复用计数出现，证明等待者都挂到了同一在途任务
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cache.GetReqQueueReuseNum() < waiters-1 {
		time.Sleep(time.Millisecond)
	}
	close(gate)
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, boom) || vs[i] != 0 {
			t.Fatalf("waiter %d got v=%d err=%v", i, vs[i], err)
		}
	}
	if got := cache.GetReqQueueReuseNum(); got < waiters-1 {
		t.Fatalf("reuse num=%d, want >= %d", got, waiters-1)
	}
	if calls.Load() != 1 {
		t.Fatalf("exactly one fetch expected, got %d", calls.Load())
	}
}

// TestHardenResourceTTLExpire 资源缓存 TTL 到期后再次访问计 miss 并重新拉取。
func TestHardenResourceTTLExpire(t *testing.T) {
	var calls atomic.Int32
	cache := NewLRUResourceCache[string, string](10, 40*time.Millisecond, func(k string) (string, error) {
		calls.Add(1)
		return "v", nil
	})

	if _, err := cache.GetResource("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetResource("k"); err != nil {
		t.Fatal(err)
	}
	if cache.CacheHit() != 1 || calls.Load() != 1 {
		t.Fatalf("second call must hit: hit=%d calls=%d", cache.CacheHit(), calls.Load())
	}
	time.Sleep(70 * time.Millisecond)
	if _, err := cache.GetResource("k"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || cache.CacheMiss() != 2 {
		t.Fatalf("expired entry must be re-fetched: calls=%d miss=%d", calls.Load(), cache.CacheMiss())
	}
}

// TestHardenResourceZeroMaxEntries maxEntries=0 时每个分片容量夹到 1，
// 缓存整体仍有界（旧实现 perSize=0 → 每分片无界增长）。
func TestHardenResourceZeroMaxEntries(t *testing.T) {
	cache := NewLRUResourceCache[int, int](0, time.Minute, func(k int) (int, error) {
		return k, nil
	})
	for i := 0; i < 2000; i++ {
		if _, err := cache.GetResource(i); err != nil {
			t.Fatal(err)
		}
	}
	if cache.Size() > runtime.NumCPU() {
		t.Fatalf("cache unbounded: size=%d > shards=%d", cache.Size(), runtime.NumCPU())
	}
}
