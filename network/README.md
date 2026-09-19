# network

[中文文档](README_ZH.md)

`network` provides a unified, cross-protocol network abstraction layer (`Conn`) along with framing, flow and congestion control, and application-layer proxies. It enables transparent protocol switching across various transport mechanisms—from standard stream sockets to custom reliable channels layered over raw datagrams.

---

## Table of Contents

- [Key Architecture](#key-architecture)
  - [1. Unified Connection Abstraction (`Conn`)](#1-unified-connection-abstraction-conn)
  - [2. Reliable Frame Manager (`FrameMgr`)](#2-reliable-frame-manager-framemgr)
  - [3. Bandwidth-Based Congestion Control (`BBCongestion`)](#3-bandwidth-based-congestion-control-bbcongestion)
  - [4. SOCKS5 Proxy Implementation](#4-socks5-proxy-implementation)
- [Supported Protocols](#supported-protocols)
- [Usage Examples](#usage-examples)
  - [Echo Server & Client (KCP / QUIC / TCP / RUDP)](#echo-server--client-kcp--quic--tcp--rudp)
  - [SOCKS5 Server with Password Auth](#socks5-server-with-password-auth)
- [Reliable Protocol Download Throughput Benchmark](#reliable-protocol-download-throughput-benchmark)
  - [Methodology](#methodology)
  - [Simulated Link Conditions](#simulated-link-conditions)
  - [Execution](#execution)
  - [Test Environment](#test-environment)
  - [Results: Download Speed (MB/s)](#results-download-speed-mbs)
  - [Key Takeaways](#key-takeaways)

---

## Key Architecture

### 1. Unified Connection Abstraction (`Conn`)

The core abstraction is the `Conn` interface (`network/conn.go`), which unifies connection establishment and bidirectional streaming:

```go
type Conn interface {
    io.ReadWriteCloser
    Name() string
    Info() string
    Dial(dst string) (Conn, error)
    Listen(dst string) (Conn, error)
    Accept() (Conn, error)
}
```

- **Factory Creation**: Use `NewConn(proto)` (case-insensitive) to instantiate any supported protocol driver.
- **Dialer Interception**: Register a global socket setup controller using `RegisterDialerController(fn)` to set custom socket options before connecting.
- **Protocol Discovery**: Query available protocols with `SupportProtos()` or check reliable transports with `SupportReliableProtos()`.

### 2. Reliable Frame Manager (`FrameMgr`)

`FrameMgr` is the underlying engine for custom reliable protocols (`rudp` and `ricmp`). It implements:
- **Sliding Window ARQ**: Packet sequencing with wrap-around-aware ring indexing (`sendIdRank`), selective ACKs, and hole-targeted retransmission requests (`reqmap`).
- **Dynamic Compression**: Automatic inline payload compression using **Zstd** when payload size exceeds configurable thresholds.
- **Keep-Alive & RTT Tracking**: Periodic ping/pong and heartbeat messages to track latency and detect stale connections.

### 3. Bandwidth-Based Congestion Control (`BBCongestion`)

A rate-based congestion control module implementing the `Congestion` interface:
- Tracks the minimum delivery rate across a sliding window (`bbc_win = 5`).
- Dynamically adapts inflight limits to balance bandwidth saturation against bufferbloat.

### 4. SOCKS5 Proxy Implementation

Standard-compliant implementation of **RFC 1928** and **RFC 1929**:
- Supports `0x00` (No Authentication) and `0x02` (Username/Password authentication), with `0xFF` returned when no acceptable methods match.
- Parses and generates IPv4, IPv6, and Domain Name (`0x03`) addressing for SOCKS5 CONNECT requests.
- Handles connect response generation (`Sock5SendConnectReply`).

---

## Supported Protocols

| Protocol | Category | Description | Underlying Socket |
|---|---|---|---|
| **`tcp`** | Reliable | Standard Go `net.TCPConn` wrapper with cancellable dials. | TCP Stream |
| **`udp`** | Unreliable | Datagram transport supporting session emulation with per-peer receive channels. | UDP Datagram |
| **`kcp`** | Reliable | Low-latency ARQ protocol (`kcp-go`) tuned with fast-retransmit (`NoDelay`) and MTU 1200. | UDP Datagram |
| **`quic`** | Reliable | Modern multiplexed transport (`quic-go`) with single-stream abstraction and explicit UDP socket lifecycle. | UDP Datagram |
| **`rudp`** | Reliable | Lightweight custom reliable UDP powered by `FrameMgr` and `BBCongestion` (uses `sendmmsg` batching on Linux). | UDP Datagram |
| **`ricmp`** | Reliable | Covert reliable transport over ICMP Echo packets (requires `CAP_NET_RAW` privileges). | Raw ICMP Socket |
| **`rhttp`** | Reliable | Reliable bi-directional data tunnel layered over repeated HTTP requests with connection pooling and server timeouts. | HTTP / TCP |

---

## Usage Examples

### Echo Server & Client (KCP / QUIC / TCP / RUDP)

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	proto := "kcp" // easily switch to "tcp", "rudp", "quic", etc.

	// 1. Listen
	serverDriver, _ := network.NewConn(proto)
	listener, err := serverDriver.Listen("127.0.0.1:9090")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("[Server] received: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// 2. Dial
	clientDriver, _ := network.NewConn(proto)
	conn, err := clientDriver.Dial("127.0.0.1:9090")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	conn.Write([]byte("ping"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("[Client] response: %s\n", string(buf[:n]))
}
```

### SOCKS5 Server with Password Auth

```go
package main

import (
	"net"
	"github.com/esrrhs/gohome/network"
)

func handleClient(conn net.Conn) {
	defer conn.Close()

	// Perform RFC 1928 / RFC 1929 Handshake
	if err := network.Sock5HandshakeBy(conn, "myuser", "mypassword"); err != nil {
		return
	}

	// Parse CONNECT target
	_, targetHost, err := network.Sock5GetRequest(conn)
	if err != nil {
		return
	}

	// Forward to remote target
	remote, err := net.Dial("tcp", targetHost)
	if err != nil {
		_ = network.Sock5SendConnectReply(conn, 0x05, "0.0.0.0:0") // 0x05 = Connection refused
		return
	}
	defer remote.Close()

	// Send success reply
	_ = network.Sock5SendConnectReply(conn, 0x00, remote.LocalAddr().String())

	// Relay traffic bidirectionally (io.Copy...)
}
```

---

## Reliable Protocol Download Throughput Benchmark

Measures single-connection download throughput (**rudp → kcp → quic → rhttp → ricmp**) in MB/s under simulated real-world wide-area network impairments.

### Methodology

1. Spin up an in-process server and client.
2. The server continuously `Write`s identical 1024-byte payloads.
3. The client continuously `Read`s, verifying chunk content consistency and accumulating only valid payload bytes.
4. Sustained test duration: **60 seconds**.
5. Metric: `MB/s = verified_bytes / 60 / (1024 * 1024)`.

*Note: `FrameMgr` is an internal engine for rudp/ricmp and is not measured independently. Raw `tcp` is omitted. `ricmp` requires `CAP_NET_RAW`.*

### Simulated Link Conditions

| Parameter | Value |
|---|---|
| Connections | 1 |
| One-way Delay | 200 ms (RTT ≈ 400 ms) |
| Packet Loss | 0% / 10% / 50% |
| Payload Chunk | Fixed 1024 bytes (integrity verified) |
| Duration | 60s |
| Impairment Simulation | `rudp` / `kcp` / `quic`: User-space **UDP** netem proxy; `rhttp`: Kernel **tc netem** (TCP port filtered); `ricmp`: Kernel **tc netem** (ICMP protocol filtered) |

*Dependencies: `iproute-tc`, `kernel-modules-extra` (`sch_netem`), root privileges required. tc netem configured with `limit 100000` to prevent unintended buffer exhaustion.*

### Execution

```bash
# Run all reliable throughput benchmarks:
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput -benchtime=1x -timeout 90m -benchmem

# Run ricmp or rhttp individually:
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/ricmp -benchtime=1x -timeout 30m
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/rhttp -benchtime=1x -timeout 30m
```

### Test Environment

| Item | Value |
|---|---|
| OS / Arch | Linux / amd64 |
| CPU | AMD EPYC 7K62 48-Core Processor |
| Date | 2026-09-19 |

### Results: Download Speed (MB/s)

| Protocol | 0% Loss | 10% Loss | 50% Loss |
|---|---:|---:|---:|
| **`rudp`** | 4.05 | 2.47 | 0.54 |
| **`kcp`** | 7.39 | 5.13 | 1.83 |
| **`quic`** | 0.08 | 0.02 | 0.00 |
| **`rhttp`** | 0.08 | 0.06 | *Skip* |
| **`ricmp`** | 5.07 | 2.51 | 0.00 |

- **`rhttp @ 50%`**: Skipped due to handshake timeouts on short HTTP request cycles under extreme packet loss.
- **`ricmp`**: Uses `FrameMgr` identical to `rudp`, but supports unidirectional ICMP downstream push; impaired via kernel `tc netem`.

### Key Takeaways

1. **Clean Link (0% Loss)**: **`kcp`** leads in throughput. **`ricmp`** (5.07 MB/s) and **`rudp`** (4.05 MB/s) achieve high performance in the same tier. `rhttp` and `quic` exhibit lower sustained single-stream throughput under high RTT.
2. **Moderate Loss (10% Loss)**: **`kcp`** maintains high throughput (~5.13 MB/s). **`ricmp`** and **`rudp`** perform similarly (~2.5 MB/s).
3. **Extreme Loss (50% Loss)**: **`kcp`** remains resilient (~1.83 MB/s), whereas `ricmp` drops to near zero and `rhttp` skips due to dial handshakes failing.
4. **Push vs. Poll**: Unlike `rhttp` (which requires client polling), `ricmp` allows the server to proactively push downstream ICMP packets to the client directly.
