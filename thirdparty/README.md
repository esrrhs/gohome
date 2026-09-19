# thirdparty

[中文文档](README_ZH.md)

`thirdparty` provides adapters and utility clients for popular third-party tools, including offline MaxMind GeoIP2 country lookup and MySQL auto-expiring key-value tables.

---

## Features

- **`GeoIP2`**: Fast offline IP-to-country lookup using MaxMind `GeoLite2-Country.mmdb`.
- **`TMysql`**: Lightweight MySQL database wrapper providing auto-table creation and sliding-window timestamp-based data expiration.

---

## Usage

### 1. GeoIP2 Country Lookup

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/thirdparty"
)

func main() {
	// Load the MMDB database file
	err := thirdparty.LoadGeoip2("./GeoLite2-Country.mmdb")
	if err != nil {
		panic(err)
	}

	// Lookup ISO country code (e.g. "US", "CN")
	isoCode, err := thirdparty.GetGeoipCountryIsoCode("8.8.8.8")
	fmt.Println("Country ISO Code:", isoCode)

	// Lookup English country name (e.g. "United States")
	name, err := thirdparty.GetGeoipCountryName("8.8.8.8")
	fmt.Println("Country Name:", name)
}
```

### 2. TMysql Auto-Retaining Table

```go
package main

import (
	"github.com/esrrhs/gohome/thirdparty"
)

func main() {
	// DSN, max connection count, table name, retention period (days)
	tm := thirdparty.NewTMysql("root:pass@tcp(127.0.0.1:3306)/mydb", 10, "audit_log", 30)
	if err := tm.Load(); err != nil {
		panic(err)
	}

	// Insert data; records older than 30 days are automatically purged
	tm.Insert("session_key", []byte("payload"))
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
