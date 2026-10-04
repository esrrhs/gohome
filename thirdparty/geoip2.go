package thirdparty

import (
	"errors"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

/*
geoip2 提供了一组用于获取地理位置信息的功能，基于 MaxMind 的 GeoLite2 数据库。
该包支持通过 IP 地址查询国家的 ISO 代码和名称。

功能包括：

- 加载 GeoLite2 数据库文件
- 解析 IP 地址以验证有效性
- 获取特定 IP 地址的国家 ISO 代码
- 获取特定 IP 地址的国家名称（支持英文）
- 处理在解析过程中可能出现的错误
*/

// ErrGeoipNotLoaded is returned when a lookup runs before LoadGeoip2 succeeded.
var ErrGeoipNotLoaded = errors.New("geoip2: LoadGeoip2 must be called first")

// mu guards gdb so a concurrent reload cannot swap the reader out from under
// an in-flight lookup, and so -race stays clean when reloading at runtime.
var (
	mu  sync.RWMutex
	gdb *geoip2.Reader
)

// LoadGeoip2 opens the MaxMind database at file (default
// ./GeoLite2-Country.mmdb). It replaces any previously loaded reader, closing
// the old one so repeated reloads do not leak the mmap. On failure the
// previously loaded reader stays in place.
func LoadGeoip2(file string) error {

	if len(file) <= 0 {
		file = "./GeoLite2-Country.mmdb"
	}

	db, err := geoip2.Open(file)
	if err != nil {
		return err
	}

	mu.Lock()
	old := gdb
	gdb = db
	mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

// CloseGeoip2 releases the loaded database.
func CloseGeoip2() error {
	mu.Lock()
	old := gdb
	gdb = nil
	mu.Unlock()

	if old == nil {
		return nil
	}
	return old.Close()
}

// reader returns the loaded reader, or ErrGeoipNotLoaded.
func reader() (*geoip2.Reader, error) {
	mu.RLock()
	defer mu.RUnlock()
	if gdb == nil {
		return nil, ErrGeoipNotLoaded
	}
	return gdb, nil
}

// geoCountry is the subset of the MaxMind country record the package exposes.
// geoip2.City.Country is an anonymous struct, so this keeps the accessors
// typed without depending on the library's internal layout.
type geoCountry struct {
	IsoCode string
	Name    string
}

// country looks up the country record for ipaddr. City() is used rather than
// Country() because geoip2-golang maps GeoLite2-Country onto isCity|isCountry,
// so both work; City() additionally covers City databases.
func country(ipaddr string) (geoCountry, error) {
	r, err := reader()
	if err != nil {
		return geoCountry{}, err
	}

	ip := net.ParseIP(ipaddr)
	if ip == nil {
		return geoCountry{}, errors.New("ip " + ipaddr + " ParseIP nil")
	}

	record, err := r.City(ip)
	if err != nil {
		return geoCountry{}, err
	}
	return geoCountry{IsoCode: record.Country.IsoCode, Name: record.Country.Names["en"]}, nil
}

func GetGeoipCountryIsoCode(ipaddr string) (string, error) {

	c, err := country(ipaddr)
	if err != nil {
		return "", err
	}

	return c.IsoCode, nil
}

func GetGeoipCountryName(ipaddr string) (string, error) {

	c, err := country(ipaddr)
	if err != nil {
		return "", err
	}

	return c.Name, nil
}
