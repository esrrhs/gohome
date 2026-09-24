# GoHome

[<img src="https://img.shields.io/github/license/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/languages/top/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/gohome/go.yml?branch=master">](https://github.com/esrrhs/gohome/actions)
[<img src="https://img.shields.io/badge/go-%3E%3D1.24-blue">](https://golang.org/)

[中文文档](README_ZH.md)

GoHome is a production-ready, batteries-included general-purpose infrastructure and core algorithm library for Go. It provides unified multi-protocol networking, structured concurrency, low-contention caches, cryptographic suites, and engineering utilities.

---

## Modules Overview

Each module contains its own dedicated, detailed documentation. Click any module below to view its design, architecture, and complete usage guide:

| Module | Description | Documentation |
|---|---|---|
| **[`network`](network/)** | Unified multi-protocol connection abstraction (`Conn` supporting TCP, UDP, KCP, QUIC, RUDP, RICMP, RHTTP), sliding window framing (`FrameMgr`), adaptive congestion control (`BBCongestion`), and standard SOCKS5 proxy. | [English](network/README.md) \| [中文](network/README_ZH.md) |
| **[`lru`](lru/)** | Generic-based LRU caches, multi-sharded low-contention `LRUMultiCache`, and single-flight request-merging `LRUResourceCache`. | [English](lru/README.md) \| [中文](lru/README_ZH.md) |
| **[`thread`](thread/)** | Hierarchical goroutine trees (`Group`) with cascading cancellation, CPU-bound batch worker pools (`TaskPool`), and channel-backed thread pools (`ThreadPool`). | [English](thread/README.md) \| [中文](thread/README_ZH.md) |
| **[`pool`](pool/)** | Reusable object pool (`Pool`) for GC reduction and channel-based token resource pools (`TokenPool`) for rate limiting and concurrency slots. | [English](pool/README.md) \| [中文](pool/README_ZH.md) |
| **[`list`](list/)** | Thread-safe circular byte ring buffer (`RBuffergo`), ID-indexed ring buffer (`ROBuffergo`), ring queue (`Rlistgo`), single-flight deduplication queue (`ReqQueue`), and concurrent list (`synclist`). | [English](list/README.md) \| [中文](list/README_ZH.md) |
| **[`crypto`](crypto/)** | Full CryptoNight algorithm suite (all variants: cn/0, cn/1, cn/2, cn/r, cn-lite, cn-heavy, cn-pico) and underlying cryptographic primitives (AES, Blake256, Groestl, JH, RIPEMD160, Skein). | [English](crypto/README.md) \| [中文](crypto/README_ZH.md) |
| **[`loggo`](loggo/)** | Leveled logger with ANSI true-color terminal output, automatic daily rotation, retention policy cleanup (`MaxDay`), and panic stack trace recovery. | [English](loggo/README.md) \| [中文](loggo/README_ZH.md) |
| **[`common`](common/)** | Core utility toolkit: Zstd/Zlib/Gzip compression, RC4, generic hashing (`HashGeneric`), fault-tolerant JSON with `.back` mirrors, DoH resolution, root domain parser (eTLD+1), and dynamic Protobuf. | [English](common/README.md) \| [中文](common/README_ZH.md) |
| **[`platform`](platform/)** | Cross-platform shell command execution (`ShellRunCommand`), script execution with timeout contexts (`ShellRunTimeout`), and binary process execution (`ShellRunExe`). | [English](platform/README.md) \| [中文](platform/README_ZH.md) |
| **[`dns`](dns/)** | Modern smart DNS resolution and routing: zero-config defaults, parallel dual-stack queries, optional Fake-IP mode (RFC 198.18.0.0/15) with reverse lookup, DoH/DoT over proxy, TTL cache with singleflight, and atomic hot-reloadable rules. | [English](dns/README.md) \| [中文](dns/README_ZH.md) |
| **[`thirdparty`](thirdparty/)** | Third-party adapters: offline MaxMind GeoIP2 country resolution and MySQL key-value table with automated rolling retention. | [English](thirdparty/README.md) \| [中文](thirdparty/README_ZH.md) |

---

## Quick Start

### Installation

```bash
go get -u github.com/esrrhs/gohome
```

### Quick Example: Unified Network Connection

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	// Easily switch protocols: "tcp", "udp", "kcp", "quic", "rudp", "ricmp", "rhttp"
	proto := "kcp"

	// 1. Server listens
	driver, _ := network.NewConn(proto)
	l, err := driver.Listen("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer l.Close()

	go func() {
		conn, _ := l.Accept()
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("Server received: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// 2. Client dials
	clientDriver, _ := network.NewConn(proto)
	c, err := clientDriver.Dial("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer c.Close()

	c.Write([]byte("ping"))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	fmt.Printf("Client response: %s\n", string(buf[:n]))
}
```

For more detailed guides and examples, please navigate into the respective submodule directories above.

---

## License

This project is licensed under the [MIT License](LICENSE).
