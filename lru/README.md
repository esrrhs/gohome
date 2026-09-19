# lru

[中文文档](README_ZH.md)

`lru` provides high-concurrency, generic-based Least Recently Used (LRU) cache implementations in Go, featuring single-instance LRU, multi-sharded LRU for low lock contention, and request-deduplicating resource cache.

---

## Features

- **Generic Implementation**: Type-safe keys and values (`[K comparable, V any]`).
- **TTL Support**: Optional time-to-live expiration per entry.
- **`LRUMultiCache`**: Sharded LRU distributing keys across $N$ independent internal caches using generic hashing (`HashGeneric`), drastically reducing lock contention on multi-core systems.
- **`LRUResourceCache`**: Integrates `ReqQueue` to merge and deduplicate concurrent external fetches upon cache misses (single-flight anti-stampede pattern).

---

## Architecture & Components

### 1. `LRUCache[K, V]`
The classic LRU cache backed by `sync.Mutex`, a hash map, and `container/list`.

```go
cache := lru.NewLRUCache[string, int](1000, 10*time.Minute)
cache.Set("key1", 42)
val, ok := cache.Get("key1")
```

### 2. `LRUMultiCache[K, V]`
A multi-partition cache that slices capacity across $N$ sub-caches. Ideal for high read/write concurrency.

```go
// 8 shards, total capacity 10,000, TTL 5 minutes
multi := lru.NewLRUMultiCache[string, string](8, 10000, 5*time.Minute)
multi.Set("user_100", "alice")
```

### 3. `LRUResourceCache[K, V]`
When a key is missing, concurrent calls for the same key collapse into a single execution of the fetcher function.

```go
resCache := lru.NewLRUResourceCache[string, []byte](1000, 10*time.Minute, func(key string) ([]byte, error) {
    // Heavy remote DB or HTTP call
    return fetchRemoteData(key)
})

data, err := resCache.GetResource("avatar_123")
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
