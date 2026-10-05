package fakeip

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"time"
)

// defaultFakeIPCIDR 是 Config.CIDR 缺省（或非法）时使用的默认网段
// (RFC 2544 Benchmark 测试保留段: 198.18.0.1 ~ 198.19.255.254)
const defaultFakeIPCIDR = "198.18.0.0/15"

// FakeIPPool 管理一段保留地址池中的虚拟分配，支持双向映射、反查与过期淘汰。
// 默认使用 RFC 2544 Benchmark 测试保留段 198.18.0.0/15，可通过 Config.CIDR 自定义。
type FakeIPPool struct {
	mu         sync.RWMutex
	cidr       string
	ipnet      *net.IPNet
	baseIP     uint32
	maxOffset  uint32
	current    uint32
	ttl        time.Duration
	domainToIP map[string]*entry
	ipToDomain map[string]*entry
}

type entry struct {
	domain   string
	ip       net.IP
	expireAt time.Time
}

// Config FakeIP 地址池配置
type Config struct {
	// CIDR 地址段，默认 "198.18.0.0/15"
	CIDR string
	// IP 映射有效保留时间，默认 2 小时
	TTL time.Duration
}

// NewFakeIPPool 创建 Fake-IP 地址池 (RFC 2544 Benchmark 测试保留段: 198.18.0.1 ~ 198.19.255.254)
func NewFakeIPPool(cfgs ...Config) *FakeIPPool {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	cidr := cfg.CIDR
	if cidr == "" {
		cidr = defaultFakeIPCIDR
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}

	// 解析失败时回退到默认网段，保证地址池始终可用
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil || ipnet == nil || ipnet.IP.To4() == nil {
		cidr = defaultFakeIPCIDR
		_, ipnet, _ = net.ParseCIDR(cidr)
	}

	base := (uint32(198) << 24) | (uint32(18) << 16) | 1
	maxOffset := uint32(131070) // /15 约 131072 个 IP

	if ipnet != nil {
		ip4 := ipnet.IP.To4()
		base = binary.BigEndian.Uint32(ip4) + 1
		ones, bits := ipnet.Mask.Size()
		if bits == 32 && ones < 32 {
			total := uint32(1) << (32 - ones)
			if total > 2 {
				maxOffset = total - 2
			}
		}
	}
	// 兜底：maxOffset 参与取模运算，必须大于 0
	if maxOffset == 0 {
		maxOffset = 1
	}

	return &FakeIPPool{
		cidr:       cidr,
		ipnet:      ipnet,
		baseIP:     base,
		maxOffset:  maxOffset,
		current:    0,
		ttl:        ttl,
		domainToIP: make(map[string]*entry),
		ipToDomain: make(map[string]*entry),
	}
}

// CIDR 返回地址池实际使用的网段（配置非法时回退为默认网段）
func (p *FakeIPPool) CIDR() string {
	return p.cidr
}

// IsFakeIP 判断 IP 是否落在本地址池的网段内。
// 与包级 IsFakeIP 的区别：包级函数只认默认的 198.18.0.0/15，
// 本方法跟随 Config.CIDR，自定义网段时应使用本方法。
func (p *FakeIPPool) IsFakeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if p.ipnet != nil {
		return p.ipnet.Contains(ip4)
	}
	return IsFakeIP(ip4)
}

// Len 返回当前有效的域名映射数量
func (p *FakeIPPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.domainToIP)
}

// PurgeExpired 清理所有已过期的双向映射，返回被清理的条目数。
// 过期条目默认只在被撞到时才惰性回收，长时间运行会持续占用内存，
// 建议定期调用或在域名数量增长到接近地址池容量时调用。
func (p *FakeIPPool) PurgeExpired() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.purgeExpiredLocked()
}

// purgeExpiredLocked 清理过期条目，调用方必须已持有写锁
func (p *FakeIPPool) purgeExpiredLocked() int {
	now := time.Now()

	n := 0
	for d, e := range p.domainToIP {
		if now.After(e.expireAt) {
			delete(p.domainToIP, d)
			ipStr := e.ip.String()
			if cur, ok := p.ipToDomain[ipStr]; ok && cur == e {
				delete(p.ipToDomain, ipStr)
			}
			n++
		}
	}
	return n
}

// NormalizeDomain 标准化域名
func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.Trim(domain, "."))
}

// Allocate 为域名分配或获取已存在的 Fake-IP
func (p *FakeIPPool) Allocate(domain string) net.IP {
	d := NormalizeDomain(domain)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	// 0. 映射数量逼近地址池容量时先清理过期条目，避免 domainToIP 无界增长
	if len(p.domainToIP) >= int(p.maxOffset) {
		p.purgeExpiredLocked()
	}

	// 1. 检查是否存在未过期的映射
	if e, ok := p.domainToIP[d]; ok {
		if now.Before(e.expireAt) {
			e.expireAt = now.Add(p.ttl)
			return e.ip
		}
		// 过期则清理旧映射
		delete(p.domainToIP, d)
		delete(p.ipToDomain, e.ip.String())
	}

	// 2. 循环分配一个未被占用的 IP
	for attempts := uint32(0); attempts < p.maxOffset; attempts++ {
		p.current = (p.current + 1) % p.maxOffset
		ipNum := p.baseIP + p.current

		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, ipNum)
		ip := net.IP(ipBytes)
		ipStr := ip.String()

		// 检查该 IP 是否已被占用且未过期
		if existing, ok := p.ipToDomain[ipStr]; ok {
			if now.Before(existing.expireAt) {
				continue // 仍有效，顺延寻找下一个 IP
			}
			// 已过期，清理旧的反向映射
			delete(p.domainToIP, existing.domain)
			delete(p.ipToDomain, ipStr)
		}

		// 分配成功
		e := &entry{
			domain:   d,
			ip:       ip,
			expireAt: now.Add(p.ttl),
		}
		p.domainToIP[d] = e
		p.ipToDomain[ipStr] = e
		return ip
	}

	// 若地址空间完全占满，强制回收当前游标位置
	p.current = (p.current + 1) % p.maxOffset
	ipNum := p.baseIP + p.current
	ipBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ipBytes, ipNum)
	ip := net.IP(ipBytes)
	ipStr := ip.String()

	if old, ok := p.ipToDomain[ipStr]; ok {
		delete(p.domainToIP, old.domain)
	}
	e := &entry{
		domain:   d,
		ip:       ip,
		expireAt: now.Add(p.ttl),
	}
	p.domainToIP[d] = e
	p.ipToDomain[ipStr] = e
	return ip
}

// LookupDomainByIP 通过 Fake-IP 反查原始域名
func (p *FakeIPPool) LookupDomainByIP(ip net.IP) (string, bool) {
	if ip == nil {
		return "", false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", false
	}
	return p.LookupDomainByIPStr(ip4.String())
}

// LookupDomainByIPStr 通过 IP 字符串反查原始域名
func (p *FakeIPPool) LookupDomainByIPStr(ipStr string) (string, bool) {
	p.mu.RLock()
	e, ok := p.ipToDomain[ipStr]
	if !ok {
		p.mu.RUnlock()
		return "", false
	}
	if time.Now().After(e.expireAt) {
		p.mu.RUnlock()
		return "", false
	}
	domain := e.domain
	p.mu.RUnlock()
	return domain, true
}

// IsFakeIP 判断 IP 是否落在默认的 Fake-IP 保留网段 (198.18.0.0/15)。
// 该包级函数只认默认网段；若地址池通过 Config.CIDR 使用了自定义网段，
// 请改用 FakeIPPool.IsFakeIP。
func IsFakeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19)
}

// IsFakeIPStr 判断 IP 字符串是否是 Fake-IP
func IsFakeIPStr(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return IsFakeIP(ip)
}
