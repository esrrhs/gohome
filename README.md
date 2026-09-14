# GoHome

[<img src="https://img.shields.io/github/license/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/languages/top/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/gohome/go.yml?branch=master">](https://github.com/esrrhs/gohome/actions)
[<img src="https://img.shields.io/badge/go-%3E%3D1.24-blue">](https://golang.org/)

[中文文档](README_ZH.md)

GoHome is a production-ready, batteries-included general-purpose infrastructure and core algorithm library for Go. It provides a unified abstract networking layer across diverse reliable and unreliable protocols, bandwidth-based adaptive congestion control, structured concurrency and goroutine lifecycle management, low-contention multi-sharded LRU caches, cryptography suites, and a comprehensive collection of daily engineering utilities.

---

## Table of Contents

- [Features](#features)
- [Module Overview](#module-overview)
  - [network - Abstract Network Layer & Protocols](#network---abstract-network-layer--protocols)
  - [lru - High-Concurrency LRU Cache System](#lru---high-concurrency-lru-cache-system)
  - [thread - Concurrency & Goroutine Orchestration](#thread---concurrency--goroutine-orchestration)
  - [pool - Object & Token Pools](#pool---object--token-pools)
  - [list - Data Structures & Request Queues](#list---data-structures--request-queues)
  - [crypto - Cryptographic Suite](#crypto---cryptographic-suite)
  - [loggo - Colorized Logging Library](#loggo---colorized-logging-library)
  - [common - Core Utilities & Helpers](#common---core-utilities--helpers)
  - [platform - Cross-Platform Execution & Shell Commands](#platform---cross-platform-execution--shell-commands)
  - [thirdparty - Third-Party Integrations](#thirdparty---third-party-integrations)
- [Getting Started](#getting-started)
  - [Installation](#installation)
  - [Unified Network Connection Example](#unified-network-connection-example)
  - [Multi-Sharded LRU Cache Example](#multi-sharded-lru-cache-example)
  - [Hierarchical Goroutine Management Example](#hierarchical-goroutine-management-example)
- [License](#license)

---

## Features

- **Unified Multi-Protocol Abstraction**: Seamlessly switch between TCP, UDP, KCP, QUIC, RUDP, RICMP, and RHTTP via the unified `Conn` interface.
- **Reliable Transmission Control**: Built-in sliding window protocol, sequence-based frame orchestration (`FrameMgr`), and adaptive congestion control (`BBCongestion`).
- **High-Performance Caching**: Generic-based LRU cache, multi-sharded `LRUMultiCache` for near-zero lock contention, and `LRUResourceCache` with automatic deduplication of in-flight misses.
- **Structured Concurrency**: Hierarchical parent-child goroutine management (`Group`), worker-based CPU-intensive task pool (`TaskPool`), and channel-bound worker thread pool (`ThreadPool`).
- **Cryptographic Suite**: Full implementation of CryptoNight algorithm variants alongside cryptographic primitives including AES, Blake256, Groestl, JH, RIPEMD160, SHA-3, Skein, and Threefish.
- **Robust Utility Toolkit**: Fault-tolerant JSON serialization with `.back` backups, DoH (DNS-over-HTTPS) resolution, eTLD+1 root domain extraction, Zlib/Gzip streaming, RC4 encryption, and endianness detection.

---

## Module Overview

### network - Abstract Network Layer & Protocols
- **Unified Connection (`Conn`)**: Uniform API implementing `Dial`, `Listen`, `Accept`, and standard `io.ReadWriteCloser`.
- **Supported Protocols**:
  - `tcp`: Standard TCP streams.
  - `udp`: UDP transport with session simulation.
  - `kcp`: ARQ-based low-latency reliable transport.
  - `quic`: Modern QUIC multiplexed streams.
  - `rudp`: Lightweight reliable UDP protocol.
  - `ricmp`: Covert reliable channel over ICMP echo packets.
  - `rhttp`: Reliable tunneling over standard HTTP.
- **Frame Control (`FrameMgr`)**: Frame sequencing, dynamic ACK feedback, sliding window buffers, heartbeat timeouts, and connection liveness verification.
- **Congestion Control (`BBCongestion`)**: Real-time bandwidth probing and inflight window scaling algorithm.
- **SOCKS5 Proxy**: Built-in handshake and request forwarding implementation for SOCKS5 client and server.

### lru - High-Concurrency LRU Cache System
- **`LRUCache[K, V]`**: Classic doubly-linked list and hash map implementation using Go generics, supporting TTL expiration.
- **`LRUMultiCache[K, V]`**: Partitioned multi-layer LRU hashing keys across isolated cache shards to minimize lock contention on multicore systems.
- **`LRUResourceCache[K, V]`**: Combines sharded LRU with `ReqQueue` to merge and deduplicate concurrent external fetches upon cache misses.

### thread - Concurrency & Goroutine Orchestration
- **`Group`**: Hierarchical tree-structured goroutine manager with cooperative cancellation, parent-child error propagation, and panic recovery.
- **`TaskPool`**: Thread pool designed for CPU-bound batch tasks, providing worker dispatch and channel synchronization.
- **`ThreadPool`**: Channel-backed concurrent worker pool with metrics tracking task latency, throughput, and queue depth.

### pool - Object & Token Pools
- **`Pool`**: Reusable object pool with allocation tracking and metrics to reduce garbage collection pressure.
- **`TokenPool`**: Channel-based token/slot pool for concurrency rate limiting and resource throttling.

### list - Data Structures & Request Queues
- **`RBuffergo`**: Thread-safe circular byte ring buffer with read/write bookmarking.
- **`ROBuffergo`**: Ring buffer with indexed element IDs and slots.
- **`Rlistgo`**: Fixed-capacity ring queue for arbitrary elements.
- **`ReqQueue`**: Single-flight concurrent request deduplication queue.
- **`synclist`**: Mutex-synchronized doubly linked list wrapping `container/list`.

### crypto - Cryptographic Suite
- **CryptoNight**: Full support for CryptoNight variants (cn/0, cn/1, cn/2, cn/r, cn/fast, cn/half, cn/xao, cn/rto, cn/rwz, cn/double, cn-lite series, cn-heavy series, cn-pico series, etc.).
- **Hash & Cipher Primitives**: Core modules for AES, Blake256, Groestl, JH, RIPEMD160, SHA-3, Skein, and Threefish.

### loggo - Colorized Logging Library
- Level-based log routing (`DEBUG`, `INFO`, `WARN`, `ERROR`).
- Full terminal ANSI true-color styling support.
- Daily file rotation and automatic retention purging based on `MaxDay`.
- Built-in panic interceptor dumping formatted goroutine stack traces.

### common - Core Utilities & Helpers
- **Compression**: High-performance Zlib and Gzip streaming compression.
- **Security & Encodings**: RC4 stream cipher, UUID generation, native endianness detection.
- **Math & Numeric**: Generic math helpers (`MinOfInt`, `MaxOfInt`, `AbsInt`, `SafeDivide`), pseudo-random generation, and slice shuffling.
- **Hashing**: MD5, XXHash, CRC32, FNV-64a, and generic type hasher `HashGeneric[T]`.
- **Filesystem**: Fault-tolerant JSON loader/saver with automatic `.back` mirroring, symlink-aware recursive traversal (`Walk`), MD5 hashing, and line replacement.
- **Networking & DNS**: Outbound local IP detection, private IP range checks, DoH (DNS-over-HTTPS) resolution, and eTLD+1 root domain parser.
- **Protobuf**: Dynamic file descriptor set loading, reflection introspection, and proto-to-JSON serialization.

### platform - Cross-Platform Execution & Shell Commands
- **`ShellRun` / `ShellRunTimeout`**: Execute shell scripts with structured logging and deadline contexts.
- **`ShellRunCommand`**: Execute raw shell commands and capture interleaved stdout/stderr.
- **`ShellRunExe` / `ShellRunExeTimeout`**: Spawn and monitor standalone executable processes.

### thirdparty - Third-Party Integrations
- **`GeoIP2`**: MaxMind GeoLite2 country and ISO code lookup adapter.
- **`TMysql`**: MySQL client with automatic timestamp-based record retention.

---

## Getting Started

### Installation

```bash
go get -u github.com/esrrhs/gohome
```

### Unified Network Connection Example

Create clients and servers across protocols using the unified `NewConn` API:

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	// Supported: "tcp", "udp", "rudp", "ricmp", "kcp", "quic", "rhttp"
	listener, err := network.NewConn("kcp")
	if err != nil {
		panic(err)
	}

	l, err := listener.Listen("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer l.Close()

	// Handle incoming connections
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("Received: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// Dial from client
	client, _ := network.NewConn("kcp")
	c, err := client.Dial("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer c.Close()

	c.Write([]byte("ping"))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	fmt.Printf("Echo back: %s\n", string(buf[:n]))
}
```

### Multi-Sharded LRU Cache Example

Reduce lock contention under high-throughput workloads with `LRUMultiCache`:

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/lru"
)

func main() {
	// Create cache with 8 shards, capacity=1000, TTL=10 minutes
	cache := lru.NewLRUMultiCache[string, int](8, 1000, 10*time.Minute)

	cache.Set("user_1001", 99)

	if val, ok := cache.Get("user_1001"); ok {
		fmt.Printf("Cached value: %d\n", val)
	}
}
```

### Hierarchical Goroutine Management Example

Coordinate structured goroutines cleanly with `thread.Group`:

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/thread"
)

func main() {
	rootGroup := thread.NewGroup("root", nil, func() {
		fmt.Println("Root group exited")
	})

	// Spawn worker task
	rootGroup.Go(func() {
		fmt.Println("Processing worker job...")
		time.Sleep(100 * time.Millisecond)
	})

	// Graceful shutdown
	rootGroup.Exit()
	rootGroup.Wait()
}
```

---

## License

This project is licensed under the [MIT License](LICENSE).
