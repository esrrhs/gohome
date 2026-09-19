# lru

[English](README.md)

`lru` 提供了基于 Go 泛型的高性能最近最少使用（LRU）缓存实现，包含经典单例 LRU 缓存、针对多核并发优化的多分片低锁竞争缓存，以及带并发请求合并防击穿的资源缓存。

---

## 核心特性

- **类型安全**：基于 Go 泛型实现（`[K comparable, V any]`）。
- **TTL 过期支持**：可为缓存项配置全局有效期，访问过期数据自动触发清理。
- **`LRUMultiCache`（分片缓存）**：根据键的哈希自动分散存储至多个独立的子 LRU 实例，大幅降低多核高并发访问下的互斥锁竞争。
- **`LRUResourceCache`（防击穿资源缓存）**：集成请求队列 `ReqQueue`，当缓存未命中时对同 Key 并发请求进行单飞合并（Single-Flight），避免瞬间穿透压垮底层存储或远端服务。

---

## 组件说明与用法

### 1. 经典 LRU 缓存：`LRUCache[K, V]`
基于互斥锁、哈希表与双向链表构建。

```go
cache := lru.NewLRUCache[string, int](1000, 10*time.Minute)
cache.Set("key1", 42)
if val, ok := cache.Get("key1"); ok {
    fmt.Println(val)
}
```

### 2. 多分片并发缓存：`LRUMultiCache[K, V]`
将总容量均匀划分到多个分片上，按 Key 散列路由：

```go
// 8 个分片，总容量 10,000，TTL 为 5 分钟
multi := lru.NewLRUMultiCache[string, string](8, 10000, 5*time.Minute)
multi.Set("user_100", "alice")
```

### 3. 并发合并资源缓存：`LRUResourceCache[K, V]`
当高并发穿透访问未命中时，自动复用首个任务的获取结果：

```go
resCache := lru.NewLRUResourceCache[string, []byte](1000, 10*time.Minute, func(key string) ([]byte, error) {
    // 耗时的远程数据库或外部 RPC 请求
    return fetchRemoteData(key)
})

data, err := resCache.GetResource("avatar_123")
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
