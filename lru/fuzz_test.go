package lru

import (
	"testing"
	"time"
)

// fuzzCapacity 把 fuzz 给的容量夹到合法范围。构造器会把 <=0 夹成 1，这里显式
// 限上界是为了控制 fuzz 的执行时间。
func fuzzCapacity(capacity uint16) int {
	n := int(capacity)
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	return n
}

// FuzzLRUCache 检查 LRU 的两条硬不变量：
//   - Size() 永远不超过容量（构造时 <=0 会被夹成 1，淘汰逻辑必须始终生效）
//   - Set 之后立即 Get 必须命中（TTL 未过期时缓存不得无故丢失写入）
func FuzzLRUCache(f *testing.F) {
	f.Add(uint16(1), []byte{1, 2, 3})
	f.Add(uint16(4), []byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add(uint16(0), []byte{9})
	f.Add(uint16(64), make([]byte, 0))

	f.Fuzz(func(t *testing.T, capacity uint16, ops []byte) {
		cap := fuzzCapacity(capacity)
		c := NewLRUCache[uint8, int](cap, time.Minute)

		for i, key := range ops {
			c.Set(key, i)

			if got, ok := c.Get(key); !ok || got != i {
				t.Fatalf("cap=%d: Set(%d,%d) then Get = (%d,%v)", cap, key, i, got, ok)
			}
			if c.Size() > cap {
				t.Fatalf("cap=%d: Size()=%d exceeds capacity", cap, c.Size())
			}
		}

		// 容量内反复写入不同 key 后，Size 仍不得超过容量
		for i := 0; i < 256 && i < cap*2; i++ {
			c.Set(uint8(i), i)
			if c.Size() > cap {
				t.Fatalf("cap=%d: Size()=%d exceeds capacity after refilling", cap, c.Size())
			}
		}

		c.Clear()
		if c.Size() != 0 {
			t.Fatalf("Size()=%d after Clear(), want 0", c.Size())
		}
		if len(ops) > 0 {
			if _, ok := c.Get(uint8(ops[0])); ok {
				t.Fatalf("Get after Clear() must miss")
			}
		}
	})
}

// FuzzLRUMultiCache 对分片版做同样检查：分片不得破坏"写入即可读"与容量上限。
func FuzzLRUMultiCache(f *testing.F) {
	f.Add(uint16(1), uint16(4), []byte{1, 2, 3})
	f.Add(uint16(8), uint16(2), []byte{5, 6, 7, 8, 9, 10})
	f.Add(uint16(0), uint16(0), []byte{1})

	f.Fuzz(func(t *testing.T, numCaches uint16, capacity uint16, ops []byte) {
		shards := fuzzCapacity(numCaches)
		cap := fuzzCapacity(capacity)
		c := NewLRUMultiCache[uint8, int](shards, cap, time.Minute)

		for i, key := range ops {
			c.Set(key, i)
			if got, ok := c.Get(key); !ok || got != i {
				t.Fatalf("shards=%d cap=%d: Set(%d,%d) then Get = (%d,%v)", shards, cap, key, i, got, ok)
			}
			if c.Size() > cap*shards {
				t.Fatalf("shards=%d cap=%d: Size()=%d exceeds %d", shards, cap, c.Size(), cap*shards)
			}
		}

		c.Clear()
		if c.Size() != 0 {
			t.Fatalf("Size()=%d after Clear(), want 0", c.Size())
		}
	})
}
