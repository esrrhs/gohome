# `gohome/dns`

`gohome/dns` 是一个专为现代化网络代理、VPN 与智能分流工具设计的高性能 DNS 解析与分流组件。它提供了开箱即用的默认规则、双栈并发防污染解析、可选的 Fake-IP 模式（带反查支持）、基于代理隧道的加密 DoH 上游，以及原子无锁热更新接口。

---

## 核心特性

1. **开箱即用（Zero-Config Defaults）**：
   - 内置国内权威 DNS（阿里、腾讯 UDP 与 DoH）及境外安全 DNS（Cloudflare、Google DoH）。
   - 内置国内常见顶级域名（`.cn`、`.中国`、`.公司`、`.网络`）以及主流国内骨干域名。
   - 内置 RFC 局域网私有保留网段（`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、CGNAT `100.64.0.0/10` 等）。
2. **可选 Fake-IP 模式（RFC 198.18.0.0/15）**：
   - 默认关闭（`EnableFakeIP: false`），满足通用真实 DNS 查询场景；
   - 开启后对代理域名实现 0ms 极速虚拟 IP 分配；
   - **完善的双向反查**：提供 `LookupDomainByFakeIP(ip)` / `LookupDomainByFakeIPStr(ipStr)`，并自动拦截处理标准 DNS `PTR` 反向查询请求；
   - 支持 TTL 与 LRU 同步生命周期清理，防止地址池复用发生串号。
3. **并发竞速防污染（Parallel Dual-Stack Query）**：
   - 未命中规则的未知域名同时向国内直连与海外安全 DoH 发起查询；
   - 国内解析回包校验 GeoIP，一旦发现境外 IP 或疑似劫持立即丢弃，无缝采纳海外 DoH 结果，彻底消灭旧式串行重试高延迟。
4. **全动态无锁热更新**：
   - `UpdateDirectIPs(cidrs []string)`：原子更新直连 CIDR 网段。
   - `UpdateDirectDomains(domains []string)` / `UpdateProxyDomains`：热重载域名规则树。
   - `LoadDirectDomainFile(filePath string)`：增量加载文件规则（兼容 dnsmasq 格式）。
   - `ReloadGeoIPDatabase(filePath string)`：热替换 GeoIP 数据库。
   - `UpdateProxyAddress(proxyURL string)`：动态切换 DoH 上游所走代理。
5. **独立服务端**：
   - 内置标准 UDP/TCP DNS 服务端（[`Server`](server.go)），支持直接在 `:53` 或自定义端口监听。

---

## 快速使用

```go
package main

import (
	"context"
	"fmt"
	"github.com/esrrhs/gohome/dns"
)

func main() {
	// 1. 使用开箱即用默认配置创建解析器
	cfg := dns.DefaultConfig()
	cfg.EnableFakeIP = true // 按需开启 Fake-IP
	r, err := dns.NewResolver(cfg)
	if err != nil {
		panic(err)
	}

	// 2. 解析域名
	ips, err := r.Resolve(context.Background(), "google.com")
	fmt.Printf("google.com IP: %v\n", ips)

	// 3. Fake-IP 反查原始域名
	if r.IsFakeIP(ips[0]) {
		domain, ok := r.LookupDomainByFakeIP(ips[0])
		fmt.Printf("反查域名: %s (ok=%v)\n", domain, ok)
	}

	// 4. 业务方定时或事件驱动热更新规则
	r.UpdateDirectDomains([]string{"custom-internal.com"})
	r.UpdateDirectIPs([]string{"192.168.0.0/16", "10.0.0.0/8"})
}
```
