package lru

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestResourceGet(t *testing.T) {
	get := func(key string) (string, error) {
		t.Logf("Fetching data for key: %s\n", key)
		time.Sleep(2 * time.Second) // 模拟延时
		return fmt.Sprintf("data-for-%s", key), nil
	}

	cache := NewLRUResourceCache[string, string](1, 1*time.Second, get)

	// 模拟并发请求
	var wg sync.WaitGroup
	keys := []string{"key1", "key1", "key2", "key3", "key1", "key2"}
	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			resource, err := cache.GetResource(k)
			if err != nil {
				t.Errorf("Error fetching resource for %s: %v\n", k, err)
				return
			}
			t.Logf("Resource for %s: %s\n", k, resource)
		}(key)
	}

	wg.Wait()
}

func TestResourceCacheHitMiss(t *testing.T) {
	get := func(key string) (string, error) {
		return fmt.Sprintf("val-%s", key), nil
	}

	cache := NewLRUResourceCache[string, string](100, time.Minute, get)

	if cache.CacheHit() != 0 || cache.CacheMiss() != 0 {
		t.Fatalf("Expected 0 hits and 0 misses initially, got hit=%d miss=%d", cache.CacheHit(), cache.CacheMiss())
	}

	// First access is always a miss
	v, err := cache.GetResource("k1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if v != "val-k1" {
		t.Fatalf("Expected 'val-k1', got '%s'", v)
	}
	if cache.CacheMiss() != 1 {
		t.Fatalf("Expected 1 miss, got %d", cache.CacheMiss())
	}

	// Second access to same key should be a hit
	v, err = cache.GetResource("k1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if v != "val-k1" {
		t.Fatalf("Expected 'val-k1', got '%s'", v)
	}
	if cache.CacheHit() != 1 {
		t.Fatalf("Expected 1 hit, got %d", cache.CacheHit())
	}

	// Access a different key — another miss
	_, _ = cache.GetResource("k2")
	if cache.CacheMiss() != 2 {
		t.Fatalf("Expected 2 misses, got %d", cache.CacheMiss())
	}

	// ResetCacheHitMiss should zero both counters
	cache.ResetCacheHitMiss()
	if cache.CacheHit() != 0 || cache.CacheMiss() != 0 {
		t.Fatalf("Expected 0/0 after reset, got hit=%d miss=%d", cache.CacheHit(), cache.CacheMiss())
	}
}

func TestResourceCacheSize(t *testing.T) {
	get := func(key int) (int, error) {
		return key * 10, nil
	}

	cache := NewLRUResourceCache[int, int](100, time.Minute, get)

	if cache.Size() != 0 {
		t.Fatalf("Expected size 0 initially, got %d", cache.Size())
	}

	for i := 1; i <= 5; i++ {
		_, err := cache.GetResource(i)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}

	if cache.Size() != 5 {
		t.Fatalf("Expected size 5, got %d", cache.Size())
	}
}

func TestResourceCacheReqQueueStats(t *testing.T) {
	get := func(key string) (string, error) {
		return fmt.Sprintf("result-%s", key), nil
	}

	cache := NewLRUResourceCache[string, string](100, time.Minute, get)

	// Initially all stats should be 0
	if cache.GetReqQueueNewNum() != 0 {
		t.Fatalf("Expected new=0 initially, got %d", cache.GetReqQueueNewNum())
	}
	if cache.GetReqQueueReuseNum() != 0 {
		t.Fatalf("Expected reuse=0 initially, got %d", cache.GetReqQueueReuseNum())
	}

	// Fetch a resource to trigger queue activity
	_, _ = cache.GetResource("x")

	newNum := cache.GetReqQueueNewNum()
	if newNum == 0 {
		t.Fatal("Expected non-zero new queue count after resource fetch")
	}

	// Reset and verify
	cache.ResetReqQueueNewNum()
	if cache.GetReqQueueNewNum() != 0 {
		t.Fatalf("Expected new=0 after reset, got %d", cache.GetReqQueueNewNum())
	}

	cache.ResetReqQueueReuseNum()
	if cache.GetReqQueueReuseNum() != 0 {
		t.Fatalf("Expected reuse=0 after reset, got %d", cache.GetReqQueueReuseNum())
	}
}
