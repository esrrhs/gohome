# network

[English](README.md)

`network` 提供了一套统一的跨协议网络抽象层（`Conn`），并集成了数据分帧管理、流量与自适应拥塞控制，以及应用层代理协议支持。它允许开发者在各种传输协议之间进行无缝切换——无论是标准流式套接字，还是基于原始数据报构建的自研可靠传输信道。

---

## 目录

- [核心架构](#核心架构)
  - [1. 统一连接抽象 (`Conn`)](#1-统一连接抽象-conn)
  - [2. 可靠分帧管理器 (`FrameMgr`)](#2-可靠分帧管理器-framemgr)
  - [3. 带宽自适应拥塞控制 (`BBCongestion`)](#3-带宽自适应拥塞控制-bbcongestion)
  - [4. SOCKS5 代理实现](#4-socks5-代理实现)
- [支持协议一览](#支持协议一览)
- [使用范例](#使用范例)
  - [Echo 服务端与客户端 (KCP / QUIC / TCP / RUDP)](#echo-服务端与客户端-kcp--quic--tcp--rudp)
  - [带密码认证的 SOCKS5 服务端](#带密码认证的-socks5-服务端)
- [可靠协议下载速度 Benchmark](#可靠协议下载速度-benchmark)
  - [测试方法](#测试方法)
  - [模拟链路环境](#模拟链路环境)
  - [运行方式](#运行方式)
  - [测试环境配置](#测试环境配置)
  - [测试结果：下载速度 (MB/s)](#测试结果下载速度-mbs)
  - [分析结论](#分析结论)

---

## 核心架构

### 1. 统一连接抽象 (`Conn`)

核心抽象定义在 `network/conn.go` 中，将握手建连与双向流式读写完整统一：

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

- **统一工厂构建**：使用 `NewConn(proto)`（不区分大小写）实例化对应协议的驱动。
- **拨号拦截与控制**：通过 `RegisterDialerController(fn)` 注册全局套接字控制器，在建连前注入底层 Socket 选项。
- **协议查询发现**：通过 `SupportProtos()` 获取支持的所有协议列表，或通过 `SupportReliableProtos()` 筛选可靠协议。

### 2. 可靠分帧管理器 (`FrameMgr`)

`FrameMgr` 是自研可靠协议（`rudp` 与 `ricmp`）的底层传输引擎，核心特性包括：
- **滑动窗口 ARQ**：支持序列号环形回绕距离计算（`sendIdRank`）、选择性确认（ACK）与空洞重传请求节流（`reqmap`）。
- **动态内联压缩**：当载荷大小超过阈值时，自动采用 **Zstd** 算法进行动态压缩与解压。
- **保活探活与 RTT 跟踪**：内置周期性 Ping/Pong 及心跳检测包，准确测量链路延迟并及时识别僵尸连接。

### 3. 带宽自适应拥塞控制 (`BBCongestion`)

基于带宽探测的自适应流控算法，实现标准 `Congestion` 接口：
- 在滑动窗口（`bbc_win = 5`）中追踪最小传输速率。
- 动态调整在途数据上限（Max In-flight），兼顾带宽充分利用与防 Bufferbloat。

### 4. SOCKS5 代理实现

完全符合 **RFC 1928** 与 **RFC 1929** 规范：
- 支持 `0x00`（无认证）与 `0x02`（用户名/密码子协商认证），无协商匹配时返回 `0xFF`。
- 支持 IPv4、IPv6 与域名（`0x03`）寻址方式的 CONNECT 请求解析与构造。
- 规范生成连接响应报文（`Sock5SendConnectReply`）。

---

## 支持协议一览

| 协议标识 | 传输特性 | 协议描述 | 底层承载 |
|---|---|---|---|
| **`tcp`** | 可靠 | 标准 Go `net.TCPConn` 封装，支持拨号超时中断。 | TCP Stream |
| **`udp`** | 不可靠 | 基础数据报传输，内置对端会话模拟及独立的接收缓冲队列。 | UDP Datagram |
| **`kcp`** | 可靠 | 基于 ARQ 的低延迟传输协议（`kcp-go`），开启 NoDelay 极速模式并调优 MTU 1200。 | UDP Datagram |
| **`quic`** | 可靠 | 现代多路复用协议（`quic-go`），单 Stream 原生传输并显式管理底层 UDP Socket 生命周期。 | UDP Datagram |
| **`rudp`** | 可靠 | 基于 `FrameMgr` 与 `BBCongestion` 的轻量可靠 UDP，在 Linux 下支持 `sendmmsg` 批量发送。 | UDP Datagram |
| **`ricmp`** | 可靠 | 基于 ICMP Echo 报文构建的隐蔽可靠通信信道（需 `CAP_NET_RAW` 特权）。 | Raw ICMP Socket |
| **`rhttp`** | 可靠 | 基于 HTTP 请求轮询与推送的可靠穿透通道，连接池复用并带超时硬限制。 | HTTP / TCP |

---

## 使用范例

### Echo 服务端与客户端 (KCP / QUIC / TCP / RUDP)

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	proto := "kcp" // 可随时替换为 "tcp", "rudp", "quic" 等

	// 1. 服务端监听
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
		fmt.Printf("[服务端] 收到数据: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// 2. 客户端连接
	clientDriver, _ := network.NewConn(proto)
	conn, err := clientDriver.Dial("127.0.0.1:9090")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	conn.Write([]byte("ping"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("[客户端] 收到响应: %s\n", string(buf[:n]))
}
```

### 带密码认证的 SOCKS5 服务端

```go
package main

import (
	"net"
	"github.com/esrrhs/gohome/network"
)

func handleClient(conn net.Conn) {
	defer conn.Close()

	// 执行 RFC 1928 / RFC 1929 握手与认证
	if err := network.Sock5HandshakeBy(conn, "myuser", "mypassword"); err != nil {
		return
	}

	// 解析目标地址
	_, targetHost, err := network.Sock5GetRequest(conn)
	if err != nil {
		return
	}

	// 连接真实目标
	remote, err := net.Dial("tcp", targetHost)
	if err != nil {
		_ = network.Sock5SendConnectReply(conn, 0x05, "0.0.0.0:0") // 0x05 = 连接失败/拒绝
		return
	}
	defer remote.Close()

	// 发送成功回复
	_ = network.Sock5SendConnectReply(conn, 0x00, remote.LocalAddr().String())

	// 随后双向转发流量 (io.Copy...)
}
```

---

## 可靠协议下载速度 Benchmark

按顺序测 **rudp → kcp → quic → rhttp → ricmp** 单连接下载吞吐（MB/s）。

### 测试方法

1. 起一个 server、一个 client。
2. server 不停 `Write` **同一块 1024 字节**（各协议载荷完全相同）。
3. client 不停 `Read`，凑满 1024 后校验内容是否一致，只累计校验通过的有效字节。
4. 跑满 **1 分钟** 后停止测试。
5. 统计公式：`MB/s = 校验通过字节 / 60 / (1024 * 1024)`。

*说明：`FrameMgr` 为 rudp/ricmp 内部核心组件，不单独列出对比。裸 `tcp` 未纳入对比。`ricmp` 需要 `CAP_NET_RAW` 权限。*

### 模拟链路环境

| 参数 | 值 |
|---|---|
| 连接数 | 1 |
| 单向延迟 | 200ms（RTT ≈ 400ms） |
| 丢包率 | 0% / 10% / 50% |
| 载荷分块 | 固定 1024 字节（严格校验内容） |
| 持续时长 | 60s |
| 模拟工具 | `rudp` / `kcp` / `quic`：用户态 **UDP** netem 代理；`rhttp`：内核 **tc netem**（按 TCP 端口过滤）；`ricmp`：内核 **tc netem**（按 ICMP 协议过滤） |

*系统依赖：`iproute-tc`、`kernel-modules-extra`（`sch_netem`），需要 root 权限。tc netem 设置 `limit 100000`，避免默认队列长度在高吞吐下产生非预期溢出丢包。*

### 运行方式

```bash
# 执行全部可靠协议基准测试：
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput -benchtime=1x -timeout 90m -benchmem

# 单独测试 ricmp 或 rhttp：
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/ricmp -benchtime=1x -timeout 30m
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/rhttp -benchtime=1x -timeout 30m
```

### 测试环境配置

| 项 | 值 |
|---|---|
| 操作系统 / 架构 | Linux / amd64 |
| CPU 型号 | AMD EPYC 7K62 48-Core Processor |
| 测试日期 | 2026-09-19 |

### 测试结果：下载速度 (MB/s)

| 协议 | 0% 丢包 | 10% 丢包 | 50% 丢包 |
|---|---:|---:|---:|
| **`rudp`** | 4.05 | 2.47 | 0.54 |
| **`kcp`** | 7.39 | 5.13 | 1.83 |
| **`quic`** | 0.08 | 0.02 | 0.00 |
| **`rhttp`** | 0.08 | 0.06 | *Skip* |
| **`ricmp`** | 5.07 | 2.51 | 0.00 |

- **`rhttp @ 50%`**：短连接 HTTP 配合极端高丢包率，握手频繁重试超时，故标记为 `Skip`。
- **`ricmp`**：与 `rudp` 共享 `FrameMgr` 引擎，但下行支持 ICMP 直接推送；链路采用内核 `tc netem` 过滤。

### 分析结论

1. **无丢包链路**：`kcp` 吞吐最快；`ricmp`（5.07 MB/s）与 `rudp`（4.05 MB/s）性能处于同一梯队；`rhttp` 与 `quic` 受限于往返延迟与短流单流机制，吞吐较低。
2. **10% 丢包链路**：`kcp` 依然维持较高吞吐（5.13 MB/s）；`ricmp` 与 `rudp` 表现相当（约 2.5 MB/s）。
3. **50% 极高丢包链路**：`kcp` 表现最顽强（1.83 MB/s）；`ricmp` 性能受损严重（接近 0.00）；`rhttp` 建连受阻直接 Skip。
4. **推送机制差异**：`ricmp` 与 `rhttp` 不同——`rhttp` 必须依赖客户端发起轮询拉取，而 `ricmp` 服务端具备直接向客户端推送 ICMP 报文的能力。
