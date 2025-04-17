package lru

import (
	"fmt"
	"testing"
	"time"
)

func TestLRU(t *testing.T) {
	lru := NewLRUCache[int, int](3, time.Second)

	lru.Set(1, 1)
	lru.Set(2, 2)
	lru.Set(3, 3)

	if v, ok := lru.Get(1); !ok || v != 1 {
		t.Errorf("Expected 1, got %d", v)
	}

	lru.Set(4, 4) // 2 should be evicted
	if _, ok := lru.Get(2); ok {
		t.Errorf("Expected 2 to be evicted")
	}

	lru.Set(5, 5) // 3 should be evicted
	if _, ok := lru.Get(3); ok {
		t.Errorf("Expected 3 to be evicted")
	}

	time.Sleep(2 * time.Second)

	if _, ok := lru.Get(1); ok {
		t.Errorf("Expected 1 to be evicted after TTL")
	}
}

func TestLRUMulti(t *testing.T) {
	lru := NewLRUMultiCache[int, int](3, 3, time.Second)

	lru.Set(1, 1)
	lru.Set(2, 2)
	lru.Set(3, 3)
	lru.Set(4, 4)
	lru.Set(5, 5)
	lru.Set(6, 6)

	v, _ := lru.Get(1)
	fmt.Println(v)
	v, _ = lru.Get(2)
	fmt.Println(v)
	v, _ = lru.Get(3)
	fmt.Println(v)
	v, _ = lru.Get(4)
	fmt.Println(v)
	v, _ = lru.Get(5)
	fmt.Println(v)
	v, _ = lru.Get(6)
	fmt.Println(v)

	fmt.Println(lru.Size())
}
