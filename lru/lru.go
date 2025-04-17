package lru

import (
	"container/list"
	"github.com/esrrhs/gohome/common"
	"sync"
	"time"
)

type LRUCache[K comparable, V any] struct {
	capacity int
	mu       sync.Mutex
	cache    map[K]*entry[K, V]
	ll       *list.List
	ttl      time.Duration // Time-to-live duration
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	element   *list.Element
	timestamp time.Time // Last set time
}

func NewLRUCache[K comparable, V any](capacity int, ttl time.Duration) *LRUCache[K, V] {
	return &LRUCache[K, V]{
		capacity: capacity,
		cache:    make(map[K]*entry[K, V]),
		ll:       list.New(),
		ttl:      ttl,
	}
}

func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, found := c.cache[key]; found {
		// Check for TTL
		if c.ttl > 0 && time.Since(e.timestamp) > c.ttl {
			c.remove(e)
			var zeroValue V
			return zeroValue, false
		}
		c.ll.MoveToFront(e.element)
		return e.value, true
	}
	var zeroValue V
	return zeroValue, false
}

func (c *LRUCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, found := c.cache[key]; found {
		// Update the existing entry
		e.value = value
		e.timestamp = time.Now()
		c.ll.MoveToFront(e.element)
		return
	}

	// Check if we need to evict an entry
	if c.ll.Len() == c.capacity {
		c.evict()
	}

	// Create a new entry
	newEntry := &entry[K, V]{key: key, value: value, timestamp: time.Now()}
	newElement := c.ll.PushFront(newEntry)
	newEntry.element = newElement
	c.cache[key] = newEntry
}

func (c *LRUCache[K, V]) evict() {
	// Remove the oldest entry from the cache
	oldestElement := c.ll.Back()
	if oldestElement != nil {
		c.ll.Remove(oldestElement)
		oldestEntry := oldestElement.Value.(*entry[K, V])
		delete(c.cache, oldestEntry.key)
	}
}

func (c *LRUCache[K, V]) remove(e *entry[K, V]) {
	c.ll.Remove(e.element)
	delete(c.cache, e.key)
}

func (c *LRUCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[K]*entry[K, V])
	c.ll.Init()
}

func (c *LRUCache[K, V]) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cache)
}

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
