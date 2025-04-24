package thirdparty

import (
	"errors"
	"github.com/oschwald/geoip2-golang"
	"net"
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

var gdb *geoip2.Reader

func LoadGeoip2(file string) error {

	if len(file) <= 0 {
		file = "./GeoLite2-Country.mmdb"
	}

	db, err := geoip2.Open(file)
	if err != nil {
		return err
	}
	gdb = db
	return nil
}

func GetGeoipCountryIsoCode(ipaddr string) (string, error) {

	ip := net.ParseIP(ipaddr)
	if ip == nil {
		return "", errors.New("ip " + ipaddr + " ParseIP nil")
	}
	record, err := gdb.City(ip)
	if err != nil {
		return "", err
	}

	return record.Country.IsoCode, nil
}

func GetGeoipCountryName(ipaddr string) (string, error) {

	ip := net.ParseIP(ipaddr)
	if ip == nil {
		return "", errors.New("ip " + ipaddr + "ParseIP nil")
	}
	record, err := gdb.City(ip)
	if err != nil {
		return "", err
	}

	return record.Country.Names["en"], nil
}
