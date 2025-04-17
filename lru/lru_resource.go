package lru

import (
	"github.com/esrrhs/gohome/list"
	"runtime"
	"sync/atomic"
	"time"
)

type LRUResourceCache[K comparable, V any] struct {
	cache     *LRUMultiCache[K, V]
	req       *list.ReqQueue[K, V]
	cacheHit  atomic.Int32
	cacheMiss atomic.Int32
}

func NewLRUResourceCache[K comparable, V any](maxEntries int, ttl time.Duration, resourceRequestFunc func(K) (V, error)) *LRUResourceCache[K, V] {
	return &LRUResourceCache[K, V]{
		cache: NewLRUMultiCache[K, V](runtime.NumCPU(), maxEntries, ttl),
		req:   list.NewReqQueue[K, V](resourceRequestFunc),
	}
}

func (rc *LRUResourceCache[K, V]) GetResource(key K) (V, error) {

	if value, ok := rc.cache.Get(key); ok {
		// 如果缓存中有，直接返回
		rc.cacheHit.Add(1)
		return value, nil
	}

	rc.cacheMiss.Add(1)

	value, err := rc.req.Submit(key)
	if err != nil {
		var zeroValue V
		return zeroValue, err
	}

	rc.cache.Set(key, value)
	return value, nil
}

func (rc *LRUResourceCache[K, V]) CacheHit() int32 {
	return rc.cacheHit.Load()
}

func (rc *LRUResourceCache[K, V]) CacheMiss() int32 {
	return rc.cacheMiss.Load()
}

func (rc *LRUResourceCache[K, V]) ResetCacheHitMiss() {
	rc.cacheHit.Store(0)
	rc.cacheMiss.Store(0)
}

func (rc *LRUResourceCache[K, V]) GetReqQueueNewNum() int {
	return rc.req.GetNewNum()
}

func (rc *LRUResourceCache[K, V]) GetReqQueueReuseNum() int {
	return rc.req.GetReuseNum()
}

func (rc *LRUResourceCache[K, V]) ResetReqQueueNewNum() {
	rc.req.ResetNewNum()
}

func (rc *LRUResourceCache[K, V]) ResetReqQueueReuseNum() {
	rc.req.ResetReuseNum()
}
