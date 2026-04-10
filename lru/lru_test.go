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

func TestLRUClear(t *testing.T) {
	lru := NewLRUCache[string, int](5, time.Minute)

	lru.Set("a", 1)
	lru.Set("b", 2)
	lru.Set("c", 3)

	if lru.Size() != 3 {
		t.Fatalf("Expected size 3 before clear, got %d", lru.Size())
	}

	lru.Clear()

	if lru.Size() != 0 {
		t.Fatalf("Expected size 0 after clear, got %d", lru.Size())
	}

	if _, ok := lru.Get("a"); ok {
		t.Error("Expected key 'a' to be gone after clear")
	}
	if _, ok := lru.Get("b"); ok {
		t.Error("Expected key 'b' to be gone after clear")
	}
	if _, ok := lru.Get("c"); ok {
		t.Error("Expected key 'c' to be gone after clear")
	}
}

func TestLRUSize(t *testing.T) {
	lru := NewLRUCache[int, string](3, time.Minute)

	if lru.Size() != 0 {
		t.Fatalf("Expected size 0 for empty cache, got %d", lru.Size())
	}

	lru.Set(1, "one")
	lru.Set(2, "two")
	if lru.Size() != 2 {
		t.Fatalf("Expected size 2, got %d", lru.Size())
	}

	lru.Set(3, "three")
	if lru.Size() != 3 {
		t.Fatalf("Expected size 3, got %d", lru.Size())
	}

	// Eviction: capacity is 3, adding a 4th should evict oldest
	lru.Set(4, "four")
	if lru.Size() != 3 {
		t.Fatalf("Expected size 3 after eviction, got %d", lru.Size())
	}

	// Updating existing key should not change size
	lru.Set(4, "four-updated")
	if lru.Size() != 3 {
		t.Fatalf("Expected size 3 after update, got %d", lru.Size())
	}
}

func TestLRUMultiClear(t *testing.T) {
	lru := NewLRUMultiCache[int, int](3, 9, time.Minute)

	for i := 0; i < 9; i++ {
		lru.Set(i, i*10)
	}

	if lru.Size() == 0 {
		t.Fatal("Expected non-zero size before clear")
	}

	lru.Clear()

	if lru.Size() != 0 {
		t.Fatalf("Expected size 0 after clear, got %d", lru.Size())
	}

	for i := 0; i < 9; i++ {
		if _, ok := lru.Get(i); ok {
			t.Errorf("Expected key %d to be gone after clear", i)
		}
	}
}

func TestLRUSetUpdate(t *testing.T) {
	lru := NewLRUCache[string, string](3, time.Minute)

	lru.Set("key", "original")
	if v, ok := lru.Get("key"); !ok || v != "original" {
		t.Fatalf("Expected 'original', got '%s' (ok=%v)", v, ok)
	}

	lru.Set("key", "updated")
	if v, ok := lru.Get("key"); !ok || v != "updated" {
		t.Fatalf("Expected 'updated', got '%s' (ok=%v)", v, ok)
	}

	// Size should remain 1 after updating the same key
	if lru.Size() != 1 {
		t.Fatalf("Expected size 1 after update, got %d", lru.Size())
	}
}
