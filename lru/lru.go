package lru

import (
	"container/list"
	"sync"
	"time"
)

/*
LRUCache 实现了基于最近最少使用（LRU）策略的缓存机制，用于高效管理内存中的数据。

该包定义了 LRUCache 结构体，其中封装了控制数据存储和缓存淘汰的逻辑，支持基于键值对的存取操作，并可以设置过期时间（TTL）。

算法原理：

LRUCache 算法通过维护一个双向链表和一个哈希表来实现数据的快速存取和管理，具体流程如下：

1. 初始化状态：算法开始时，设定缓存的最大容量和可选的过期时间（TTL），并创建存储数据的双向链表和哈希表。

2. 数据访问：当请求获取某一具体键的数据时，检查该数据是否存在于缓存中，
  - 如果存在并且未过期，则返回数据，并将该数据移动到链表的前端（表示最近使用）。
  - 如果数据存在但已过期，则将其从缓存中删除并返回未找到的状态。
  - 如果数据不存在，则返回未找到的状态。

3. 数据插入：
  - 当插入新的键值对时，先检查当前缓存容量是否已满。
  - 如果已满，则调用淘汰策略（删除最久未使用的数据）以释放空间。
  - 插入新数据后，将数据插入到双向链表的前端，并更新哈希表。

4. 数据淘汰：当缓存达到最大容量时，通过双向链表的尾部元素（即最久未使用的数据）进行数据的淘汰，确保缓存的有效性。

5. 清空缓存：提供一个清空缓存的接口以便在需要的时候快速释放所有存储的键值对，恢复初始状态。

该算法旨在通过灵活高效的缓存管理，提高数据存取的速度，减少内存的占用，同时确保旧数据不会被频繁访问所干扰。
*/

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
