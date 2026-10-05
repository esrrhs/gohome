package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// Entry 缓存条目
type Entry struct {
	Msg       *dns.Msg
	ExpiresAt time.Time
}

// DNSCache 提供带 TTL 过期淘汰和 Singleflight 防击穿的 DNS 结果缓存
type DNSCache struct {
	mu       sync.RWMutex
	items    map[string]*Entry
	sfGroup  singleflight.Group
	capacity int
}

// NewDNSCache 创建 DNSCache
func NewDNSCache(capacity int) *DNSCache {
	if capacity <= 0 {
		capacity = 2000
	}
	return &DNSCache{
		items:    make(map[string]*Entry),
		capacity: capacity,
	}
}

// KeyForMsg 为 DNS 问题生成缓存 Key
func KeyForMsg(qName string, qType, qClass uint16) string {
	d := strings.ToLower(strings.Trim(qName, "."))
	return fmt.Sprintf("%s_%d_%d", d, qType, qClass)
}

// Get 读取缓存。命中时返回副本，并把各条 Answer 的 TTL 修正为剩余存活时间，
// 避免下游拿到一个"看起来还很新鲜"的过期时间戳。
func (c *DNSCache) Get(key string) (*dns.Msg, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		return nil, false
	}
	if time.Now().After(e.ExpiresAt) {
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	resp := e.Msg.Copy()
	remaining := time.Until(e.ExpiresAt)
	c.mu.RUnlock()

	decayTTL(resp, remaining)
	return resp, true
}

// decayTTL 把消息里所有记录的 TTL 收敛到不超过 remaining
func decayTTL(msg *dns.Msg, remaining time.Duration) {
	if msg == nil || remaining <= 0 {
		return
	}
	secs := uint32(remaining.Seconds())
	if secs == 0 {
		secs = 1
	}
	for _, rr := range msg.Answer {
		if h := rr.Header(); h != nil && h.Ttl > secs {
			h.Ttl = secs
		}
	}
	for _, rr := range msg.Ns {
		if h := rr.Header(); h != nil && h.Ttl > secs {
			h.Ttl = secs
		}
	}
	for _, rr := range msg.Extra {
		if h := rr.Header(); h != nil && h.Ttl > secs {
			h.Ttl = secs
		}
	}
}

// Set 存入缓存，根据 Msg Answer 中的最小 TTL 设置过期时间
func (c *DNSCache) Set(key string, msg *dns.Msg) {
	if msg == nil || len(msg.Answer) == 0 {
		return
	}

	minTTL := uint32(60) // 默认保底 60s
	for i, ans := range msg.Answer {
		ttl := ans.Header().Ttl
		if i == 0 || ttl < minTTL {
			minTTL = ttl
		}
	}
	if minTTL < 5 {
		minTTL = 5
	} else if minTTL > 86400 {
		minTTL = 86400 // 最长 1 天
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 简易容量控制：超容时随手清理一个过期或随机项
	if len(c.items) >= c.capacity {
		now := time.Now()
		deleted := false
		for k, v := range c.items {
			if now.After(v.ExpiresAt) {
				delete(c.items, k)
				deleted = true
				break
			}
		}
		if !deleted {
			for k := range c.items {
				delete(c.items, k)
				break
			}
		}
	}

	c.items[key] = &Entry{
		Msg:       msg.Copy(),
		ExpiresAt: time.Now().Add(time.Duration(minTTL) * time.Second),
	}
}

// DoSingleFlight 包装请求，防止并发缓存击穿。
// 注意 fn 允许返回 (nil, nil)，此时本函数同样返回 (nil, nil) 而不是 panic。
func (c *DNSCache) DoSingleFlight(key string, fn func() (*dns.Msg, error)) (*dns.Msg, error) {
	v, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		return fn()
	})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	msg, ok := v.(*dns.Msg)
	if !ok {
		return nil, fmt.Errorf("singleflight: unexpected result type %T for key %s", v, key)
	}
	return msg, nil
}

// Clear 清空缓存
func (c *DNSCache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]*Entry)
	c.mu.Unlock()
}

// PurgeExpired 清理所有已过期条目，返回被清理的数量
func (c *DNSCache) PurgeExpired() int {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0
	for k, v := range c.items {
		if now.After(v.ExpiresAt) {
			delete(c.items, k)
			n++
		}
	}
	return n
}

// Len 返回当前缓存条目数（含可能已过期但尚未清理的条目）
func (c *DNSCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}
