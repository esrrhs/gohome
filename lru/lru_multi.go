package lru

import (
	"github.com/esrrhs/gohome/common"
	"time"
)

/*
LRUMultiCache 实现了一个多层级的最近最少使用（LRU）缓存系统，旨在减少在高并发环境下的锁竞争。

该包定义了 LRUMultiCache 结构体，其中封装了一组 LRUCache，利用哈希分布的策略将键值对分散存储在多个缓存中，以优化内存利用和数据存取速度。
*/

type LRUMultiCache[K comparable, V any] struct {
	caches []*LRUCache[K, V]
}

func NewLRUMultiCache[K comparable, V any](numCaches int, capacity int, ttl time.Duration) *LRUMultiCache[K, V] {
	perSize := (capacity + numCaches - 1) / numCaches
	caches := make([]*LRUCache[K, V], numCaches)
	for i := 0; i < numCaches; i++ {
		caches[i] = NewLRUCache[K, V](perSize, ttl)
	}
	return &LRUMultiCache[K, V]{caches: caches}
}

func (c *LRUMultiCache[K, V]) Get(key K) (V, bool) {
	index := common.HashGeneric(key) % uint64(len(c.caches))
	return c.caches[index].Get(key)
}

func (c *LRUMultiCache[K, V]) Set(key K, value V) {
	index := common.HashGeneric(key) % uint64(len(c.caches))
	c.caches[index].Set(key, value)
}

func (c *LRUMultiCache[K, V]) Clear() {
	for _, cache := range c.caches {
		cache.Clear()
	}
}

func (c *LRUMultiCache[K, V]) Size() int {
	size := 0
	for _, cache := range c.caches {
		size += cache.Size()
	}
	return size
}
