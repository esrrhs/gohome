# thirdparty

[中文文档](README_ZH.md)

`thirdparty` provides adapters and utility clients for popular third-party tools, including offline MaxMind GeoIP2 country lookup and MySQL auto-expiring key-value tables.

---

## Features

- **`GeoIP2`**: Fast offline IP-to-country lookup using MaxMind `GeoLite2-Country.mmdb`.
- **`TMysql`**: Lightweight MySQL database wrapper providing auto-table creation and sliding-window timestamp-based data expiration.

---

## Safety and Robustness

Every `TMysql` query passes values through bind placeholders (`?`), so caller-supplied keys, values and LIKE patterns are never concatenated into SQL text and are safe for untrusted input.

Table names cannot be bind parameters, so they are validated as plain identifiers in `Load()` (`[A-Za-z0-9_]` only, ≤ 64 chars); anything else returns an error without issuing a single statement.

When `Load()` has not run (or failed partway), the methods return `ErrNotLoaded` or a zero value instead of panicking. `Close()` releases the connection pool and is safe to call repeatedly.

`GeoIP2` lookups return `ErrGeoipNotLoaded` rather than panicking when no database is loaded. `LoadGeoip2()` closes the previous reader on reload, `CloseGeoip2()` releases it explicitly, and both are concurrency-safe.

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
	defer thirdparty.CloseGeoip2()

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
	defer tm.Close()

	// Insert data; records older than 30 days are automatically purged.
	// Both key and value are strings.
	if err := tm.Insert("session_key", "payload"); err != nil {
		panic(err)
	}

	// Existence check / row count / most recent N / fuzzy value search
	exists := tm.Has("session_key")
	total := tm.GetSize()
	recent := tm.Last(10)
	matched := tm.FindValue("pay", 20)
	_, _, _, _ = exists, total, recent, matched
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
