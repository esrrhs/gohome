package matcher

import (
	"errors"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// GeoDB 提供基于 MaxMind GeoLite2 数据库的归属地检测与热替换能力
type GeoDB struct {
	mu sync.RWMutex
	db *geoip2.Reader
}

// NewGeoDB 创建 GeoDB
func NewGeoDB() *GeoDB {
	return &GeoDB{}
}

// Open 加载或替换 GeoLite2 数据库文件
func (g *GeoDB) Open(filePath string) error {
	db, err := geoip2.Open(filePath)
	if err != nil {
		return err
	}

	// 旧库必须在写锁内回收：GetCountryCode 全程持 RLock 查询，
	// 锁外 Close（mmap 平台即 munmap）会与仍在查询旧库的 goroutine
	// 竞争，可能读到已解映射的内存导致 SIGBUS。
	g.mu.Lock()
	oldDB := g.db
	g.db = db
	if oldDB != nil {
		_ = oldDB.Close()
	}
	g.mu.Unlock()
	return nil
}

// Close 关闭数据库
func (g *GeoDB) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.db != nil {
		err := g.db.Close()
		g.db = nil
		return err
	}
	return nil
}

// GetCountryCode 获取 IP 的两位国家代码 (如 "CN", "US")
func (g *GeoDB) GetCountryCode(ip net.IP) (string, error) {
	if ip == nil {
		return "", errors.New("nil ip")
	}

	// RLock 必须覆盖整个 Country()：旧库的 Close/munmap 由 Open/Close 在
	// 写锁保护下执行，锁释放后再查询会与解映射竞争。
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.db == nil {
		return "", errors.New("geodb not loaded")
	}

	record, err := g.db.Country(ip)
	if err != nil {
		return "", err
	}
	return record.Country.IsoCode, nil
}

// GetCountryCodeStr 获取 IP 字符串对应的国家代码
func (g *GeoDB) GetCountryCodeStr(ipStr string) (string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", errors.New("invalid ip format: " + ipStr)
	}
	return g.GetCountryCode(ip)
}

// IsCountry 快速判断 IP 是否属于目标国家代码。
// targetCode 为空时一律 false：库中无归属地记录的 IP 会返回空 code，
// 不能让空配置把所有未知 IP 都判成命中。
func (g *GeoDB) IsCountry(ip net.IP, targetCode string) bool {
	if targetCode == "" {
		return false
	}
	code, err := g.GetCountryCode(ip)
	if err != nil {
		return false
	}
	return code == targetCode
}
