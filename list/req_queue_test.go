package list

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestReqQueue(t *testing.T) {
	get := func(key string) (string, error) {
		t.Logf("Fetching data for key: %s\n", key)
		time.Sleep(2 * time.Second) // 模拟延时
		return fmt.Sprintf("data-for-%s", key), nil
	}

	reqQueue := NewReqQueue(get)

	// 模拟并发请求
	var wg sync.WaitGroup
	keys := []string{"key1", "key1", "key2", "key3", "key1", "key2"}
	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			resource, err := reqQueue.Submit(k)
			if err != nil {
				t.Errorf("Error fetching resource for %s: %v\n", k, err)
				return
			}
			t.Logf("Resource for %s: %s\n", k, resource)
		}(key)
	}

	wg.Wait()
}

func TestReqQueue_Stats(t *testing.T) {
get := func(key string) (string, error) {
return "result-" + key, nil
}
q := NewReqQueue(get)

q.Submit("a")
q.Submit("b")
q.Submit("a")

if q.GetNewNum() < 2 {
t.Errorf("expected GetNewNum >= 2, got %d", q.GetNewNum())
}

q.ResetNewNum()
if q.GetNewNum() != 0 {
t.Errorf("expected GetNewNum 0 after reset, got %d", q.GetNewNum())
}

q.ResetReuseNum()
if q.GetReuseNum() != 0 {
t.Errorf("expected GetReuseNum 0 after reset, got %d", q.GetReuseNum())
}
}
