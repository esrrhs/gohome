# `gohome/dns`

`gohome/dns` is a high-performance, modern DNS resolution and smart traffic routing module for Go. It provides unified multi-protocol DNS upstream forwarding, anti-pollution parallel queries, optional Fake-IP mode (RFC 198.18.0.0/15) with reverse lookup, and atomic hot-reloadable rules.

---

## Key Features

1. **Zero-Configuration Defaults**:
   - Built-in public domestic resolvers (AliDNS, DNSPod UDP & DoH) and remote secure resolvers (Cloudflare, Google DoH).
   - Built-in domestic TLDs (`.cn`, `.中国`, etc.) and popular China main domains.
   - Built-in RFC private/LAN CIDR blocks (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, CGNAT `100.64.0.0/10`).
2. **Optional Pluggable Fake-IP Mode (RFC 198.18.0.0/15)**:
   - Off by default (`EnableFakeIP: false`).
   - When enabled, directly returns synthetic 0ms IPs for proxy-bound domains.
   - **Full Reverse Lookup**: `LookupDomainByFakeIP(ip)` / `LookupDomainByFakeIPStr(ipStr)` and automatic interception of DNS PTR reverse queries (`in-addr.arpa`).
   - Synchronized TTL and LRU expiration to eliminate address reuse collision.
3. **Anti-Pollution Parallel Queries (Dual-Stack Racing)**:
   - For unknown domains, queries domestic and remote DoH resolvers concurrently.
   - Validates domestic responses against GeoIP; if foreign IPs or poisoning are detected, automatically discards domestic results and adopts remote DoH responses.
4. **Atomic Hot-Reloading**:
   - `UpdateDirectIPs(cidrs []string)`
   - `UpdateDirectDomains(domains []string)` / `UpdateProxyDomains`
   - `LoadDirectDomainFile(filePath string)` (supports dnsmasq format)
   - `ReloadGeoIPDatabase(filePath string)`
   - `UpdateProxyAddress(proxyURL string)` (updates upstream SOCKS5/HTTP proxy for DoH on the fly)
5. **DNS Server**:
   - Standalone UDP/TCP DNS Server ([`Server`](server.go)) ready to listen on `:53` or custom ports.
   - `Start()` returns only after both the UDP and TCP listeners are actually serving; a bind failure (e.g. listening on `:53` as non-root) returns an error instead of hanging. `Stop()` is idempotent and never blocks on lingering connections.

---

## Behavior Notes (easy to get wrong)

- **`Timeout` applies to every query path**, including domains that hit the direct/proxy whitelist — those no longer fall back to each upstream's own default timeout.
- **`FakeIPRange` is honored**: `IsFakeIP()` / `ShouldProxy()` follow the CIDR actually configured on the pool. With a custom `FakeIPRange` (e.g. `10.66.0.0/16`) do not use the package-level `fakeip.IsFakeIP()`, which only knows the default `198.18.0.0/15`.
- **Hot reload only replaces your custom part**: `UpdateDirectIPs()` and `UpdateDirectDomains()` always keep the built-in defaults (reserved CIDRs, `.cn` TLDs, popular China domains) instead of wiping them.
- **Cached TTL decays**: a cache hit returns the remaining lifetime, not the upstream's original TTL, so downstream clients never see a stale timestamp.
- **`Resolve()` queries A records only** (IPv4). Use `ResolveAll()` when you also need AAAA; IPv4 results come first.
- **Resource cleanup**: `Resolver.Close()` releases the GeoIP handle; `FakeIPPool.PurgeExpired()` reclaims expired mappings (also triggered automatically when the pool approaches capacity).

---

## Quick Example

```go
package main

import (
	"context"
	"fmt"
	"github.com/esrrhs/gohome/dns"
)

func main() {
	// 1. Create resolver with zero-config defaults
	cfg := dns.DefaultConfig()
	cfg.EnableFakeIP = true // Enable Fake-IP if needed
	r, err := dns.NewResolver(cfg)
	if err != nil {
		panic(err)
	}

	// 2. Resolve domain
	ips, err := r.Resolve(context.Background(), "google.com")
	fmt.Printf("google.com IP: %v\n", ips)

	// 3. Fake-IP Reverse lookup
	if r.IsFakeIP(ips[0]) {
		domain, ok := r.LookupDomainByFakeIP(ips[0])
		fmt.Printf("Reverse lookup: %s (ok=%v)\n", domain, ok)
	}

	// 4. Hot-reload rules
	r.UpdateDirectDomains([]string{"internal.mycompany.com"})
	r.UpdateDirectIPs([]string{"192.168.0.0/16", "10.0.0.0/8"})
}
```
