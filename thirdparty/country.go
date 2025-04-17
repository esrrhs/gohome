package thirdparty

import (
	"errors"
	"github.com/oschwald/geoip2-golang"
	"net"
)

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
