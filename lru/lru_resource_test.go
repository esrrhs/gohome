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
