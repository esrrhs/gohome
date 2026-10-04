# thirdparty

[English](README.md)

`thirdparty` 提供了第三方服务与工具库的封装集成，包含 MaxMind GeoIP2 离线 IP 地理位置查询以及带数据保留天数自动清理的 MySQL 表封装。

---

## 核心功能

- **`GeoIP2`**：基于 MaxMind `GeoLite2-Country.mmdb` 数据库实现离线高速 IP 国家 ISO 简码与英文名称查询。
- **`TMysql`**：轻量级 MySQL 数据操作封装，内置表结构自检创建与按时间滑动窗口（天数）自动淘汰过期记录的能力。

---

## 安全性与健壮性

`TMysql` 的所有查询均使用参数化占位符（`?`）传值，调用方传入的 key、value 与 LIKE 模式串绝不会拼接进 SQL 文本，可安全用于不可信输入。

表名无法作为绑定参数传入，因此在 `Load()` 时按标识符规则校验（仅允许 `[A-Za-z0-9_]`，长度 ≤ 64），不合规则直接返回错误而不执行任何语句。

`TMysql` 未调用 `Load()`（或 `Load()` 中途失败）时，各方法返回 `ErrNotLoaded`（或零值）而不会 panic。`Close()` 用于释放连接池且可重复调用。

`GeoIP2` 在未成功加载数据库时返回 `ErrGeoipNotLoaded` 而非 panic；`LoadGeoip2()` 重复调用会关闭旧的 reader，`CloseGeoip2()` 用于主动释放。两者均并发安全。

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
	defer thirdparty.CloseGeoip2()

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
	defer tm.Close()

	// 插入数据（key 与 value 均为字符串）
	if err := tm.Insert("session_key", "payload"); err != nil {
		panic(err)
	}

	// 是否存在 / 总条数 / 最近 N 条 / 按 value 模糊查找
	exists := tm.Has("session_key")
	total := tm.GetSize()
	recent := tm.Last(10)
	matched := tm.FindValue("pay", 20)
	_, _, _, _ = exists, total, recent, matched
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
