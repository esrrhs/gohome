# thirdparty

[English](README.md)

`thirdparty` 提供了第三方服务与工具库的封装集成，包含 MaxMind GeoIP2 离线 IP 地理位置查询以及带数据保留天数自动清理的 MySQL 表封装。

---

## 核心功能

- **`GeoIP2`**：基于 MaxMind `GeoLite2-Country.mmdb` 数据库实现离线高速 IP 国家 ISO 简码与英文名称查询。
- **`TMysql`**：轻量级 MySQL 数据操作封装，内置表结构自检创建与按时间滑动窗口（天数）自动淘汰过期记录的能力。

---

## 使用示例

### 1. GeoIP2 离线 IP 国家解析

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/thirdparty"
)

func main() {
	// 加载本地 mmdb 数据文件
	err := thirdparty.LoadGeoip2("./GeoLite2-Country.mmdb")
	if err != nil {
		panic(err)
	}

	// 查询国家 ISO 代码 (如 "CN", "US")
	isoCode, err := thirdparty.GetGeoipCountryIsoCode("8.8.8.8")
	fmt.Println("国家代码:", isoCode)

	// 查询英文全称
	name, err := thirdparty.GetGeoipCountryName("8.8.8.8")
	fmt.Println("国家名称:", name)
}
```

### 2. TMysql 自动淘汰数据表

```go
package main

import (
	"github.com/esrrhs/gohome/thirdparty"
)

func main() {
	// DSN, 最大连接数, 存储表名, 保留天数 (超过自动清理)
	tm := thirdparty.NewTMysql("root:pass@tcp(127.0.0.1:3306)/mydb", 10, "audit_log", 30)
	if err := tm.Load(); err != nil {
		panic(err)
	}

	// 插入数据
	tm.Insert("session_key", []byte("payload"))
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
