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

	g.mu.Lock()
	oldDB := g.db
	g.db = db
	g.mu.Unlock()

	if oldDB != nil {
		_ = oldDB.Close()
	}
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

	g.mu.RLock()
	db := g.db
	g.mu.RUnlock()

	if db == nil {
		return "", errors.New("geodb not loaded")
	}

	record, err := db.Country(ip)
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

// IsCountry 快速判断 IP 是否属于目标国家代码
func (g *GeoDB) IsCountry(ip net.IP, targetCode string) bool {
	code, err := g.GetCountryCode(ip)
	if err != nil {
		return false
	}
	return code == targetCode
}
