package fakeip

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"time"
)

// FakeIPPool 管理 198.18.0.0/15 地址池中的虚拟分配，支持双向映射、反查与 LRU 过期淘汰
type FakeIPPool struct {
	mu         sync.RWMutex
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
		cidr = "198.18.0.0/15"
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}

	_, ipnet, err := net.ParseCIDR(cidr)
	base := (uint32(198) << 24) | (uint32(18) << 16) | 1
	maxOffset := uint32(131070) // /15 约 131072 个 IP

	if err == nil && ipnet != nil && ipnet.IP.To4() != nil {
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

	return &FakeIPPool{
		baseIP:     base,
		maxOffset:  maxOffset,
		current:    0,
		ttl:        ttl,
		domainToIP: make(map[string]*entry),
		ipToDomain: make(map[string]*entry),
	}
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

// IsFakeIP 判断 IP 是否落在 Fake-IP 保留网段 (198.18.0.0/15)
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
