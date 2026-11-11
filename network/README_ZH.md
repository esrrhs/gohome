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
  - [WSS 字节流传输](#wss-字节流传输)
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

`FrameMgr` 是自研可靠协议（`rudp`、`ricmp` 与 `rdns`）的底层传输引擎，核心特性包括：
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
- 支持 IPv4、IPv6 与域名（`0x03`）寻址方式的 CONNECT（`0x01`）与 UDP ASSOCIATE（`0x03`）请求解析与构造。
- 规范生成连接/关联响应报文（`Sock5SendConnectReply`）；UDP 数据报头编解码（`Sock5PackUDP` / `Sock5UnpackUDP`）。
- 客户端：`Sock5SetRequest`（TCP CONNECT）、`Sock5SetUDPRequest`（UDP ASSOCIATE，返回中继地址）。

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
| **`rdns`** | 可靠 | 基于 FrameMgr ARQ 的 DNS 隧道（TXT/NULL 查询响应）。CutSize 很小；可选 AES-GCM PSK。DNS 元数据仍可见。 | UDP DNS |
| **`wss`** | 可靠 | 验证证书的 TLS WebSocket 字节流；可配置 CA、证书、主机名和路径。[接口与可运行示例](#wss-字节流传输)。 | TLS / TCP |

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

	// 解析目标地址（CONNECT 或 UDP ASSOCIATE）
	cmd, _, targetHost, err := network.Sock5GetRequest(conn)
	if err != nil {
		return
	}
	if cmd == network.Socks5CmdUDPAssociate {
		// 监听 UDP 中继，回复 BND 地址，保持 TCP 控制连接直至关闭
		// ...
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

### WSS 字节流传输

`NewConn("wss")` 返回 `*WssConn`，沿用 `Conn` 的 `Dial` / `Listen` /
`Accept` 接口，并注册为可靠协议。现有传输协议无需修改。

- `WssConfig.TLSConfig`：使用 Go 标准 TLS 配置，默认使用系统信任根并验证 URL
  中的主机名。私有 CA 通过 `RootCAs` 配置；通过 IP 连接使用域名证书的服务时，
  通过 `ServerName` 指定证书名称。服务端配置 `Certificates` 或 `GetCertificate`。
  双向 TLS 认证使用 Go 的标准配置字段。禁止 `InsecureSkipVerify`，不回退到明文，
  最低使用 TLS 1.2。
- `Path`：精确匹配的升级路径，默认为 `/gohome`。`Dial("host:port")` 使用该路径；
  `Dial("wss://host:port/custom")` 使用 URL 中显式指定的路径。不允许重定向。
- `HandshakeTimeout`：默认为 10 秒。`DialContext` 的调用方上下文仅控制握手的取消。
  关闭工厂对象会取消其尚未完成的拨号，已返回的连接则独立管理。必须在使用前完成配置。
- 每次 `Write` 对应一条二进制 WebSocket 消息；`Read` 将消息与帧边界展开为字节流。
  空消息不会导致零进度读取，大消息采用流式处理，不需要整条消息缓冲。允许并发读写，
  同方向操作会被串行化。拒绝文本消息，禁用压缩。
- 具体连接类型提供 `SetDeadline`、`SetReadDeadline` 和 `SetWriteDeadline`。
  进行中的读写达到截止时间后，字节流会永久关闭，这是 coder/websocket 的 `NetConn`
  适配器语义；与 TCP 通常可在超时后重设截止时间不同。
  超时错误满足 `errors.Is(err, os.ErrDeadlineExceeded)`。
- `Close` 可重复调用，会立即中止读写，不排空待发送数据，也不等待 WebSocket 关闭握手。
  收到对端的正常关闭帧时，读取返回 `io.EOF`；异常断开则可能返回错误。
  关闭监听器会关闭活动连接和接收队列中的连接，并解除 `Accept` 的阻塞。
  接收队列已满时，会拒绝已升级的连接。
- 监听器的 `Addr()` 返回实际绑定的地址，包含使用端口 `0` 时由系统分配的端口。
  客户端 `Info()` 使用 WebSocket 适配器提供的合成远端地址。

#### 可运行的本地客户端与服务端

从仓库根目录执行以下命令，生成临时开发证书。不得将此示例密钥或证书用于生产环境：

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout /tmp/gohome-wss.key -out /tmp/gohome-wss.crt -days 1 \
  -subj /CN=localhost -addext subjectAltName=DNS:localhost
```

将下面的程序保存为 `/tmp/gohome-wss.go`，然后从模块根目录运行
`go run /tmp/gohome-wss.go`：

```go
package main

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "io"
    "log"
    "os"
    "time"

    "github.com/esrrhs/gohome/network"
)

func main() {
    cert, err := tls.LoadX509KeyPair("/tmp/gohome-wss.crt", "/tmp/gohome-wss.key")
    if err != nil { log.Fatal(err) }
    server, _ := network.NewConn("wss")
    sc := network.DefaultWssConfig()
    sc.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
    server.(*network.WssConn).SetConfig(sc)
    listener, err := server.Listen("127.0.0.1:0")
    if err != nil { log.Fatal(err) }
    defer listener.Close()
    done := make(chan error, 1)
    go func() {
        conn, err := listener.Accept()
        if err != nil { done <- err; return }
        defer conn.Close()
        conn.(*network.WssConn).SetDeadline(time.Now().Add(5*time.Second))
        buf := make([]byte, 5)
        if _, err = io.ReadFull(conn, buf); err == nil {
            _, err = conn.Write(buf)
        }
        done <- err
    }()
    pem, err := os.ReadFile("/tmp/gohome-wss.crt")
    if err != nil { log.Fatal(err) }
    roots := x509.NewCertPool()
    if !roots.AppendCertsFromPEM(pem) { log.Fatal("invalid CA") }
    client, _ := network.NewConn("wss")
    cc := network.DefaultWssConfig()
    cc.TLSConfig = &tls.Config{RootCAs: roots, ServerName: "localhost"}
    client.(*network.WssConn).SetConfig(cc)
    conn, err := client.Dial(listener.(*network.WssConn).Addr().String())
    if err != nil { log.Fatal(err) }
    defer conn.Close()
    conn.(*network.WssConn).SetDeadline(time.Now().Add(5*time.Second))
    if _, err = conn.Write([]byte("hello")); err != nil { log.Fatal(err) }
    buf := make([]byte, 5)
    if _, err = io.ReadFull(conn, buf); err != nil { log.Fatal(err) }
    if err = <-done; err != nil { log.Fatal(err) }
    fmt.Println(string(buf))
}
```

#### 反向代理

反向代理需要转发 HTTP/1.1 的 `Upgrade` 和 `Connection` 头，保留配置的路径
（或有意将其重写为后端路径），并允许长连接隧道。
此实现也要求代理到后端的连接使用 TLS：例如 `https://127.0.0.1:8443`。
需要在代理中安装后端 CA，并验证后端证书名称。以下 Nginx 配置片段放在已配置 TLS
的 `server` 块内：

```nginx
location = /gohome {
    proxy_pass https://127.0.0.1:8443;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_ssl_verify on;
    proxy_ssl_trusted_certificate /etc/nginx/upstream-ca.pem;
    proxy_ssl_server_name on;
    proxy_ssl_name tunnel.internal.example;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

WSS 监听器需要使用 `tunnel.internal.example` 的证书；公网客户端验证代理公网域名的
证书。这样既支持反向代理，也不会暴露未加密的后端连接或关闭证书验证。
WSS 加密应用数据，但不会隐藏 IP 地址、TLS 握手元数据、流量大小与时序，或 WebSocket
隧道的存在。它不保证无法被检测，也不保证绕过所有网络限制。应在代理处配置身份认证、
访问控制及资源限制；仅有 TLS 服务端身份认证，不等于已对客户端进行授权。

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

### FrameMgr / BBCongestion 微基准

分配热点相关的 bench 在 `framemgr_bench_test.go`，适合对比 cut/stage/`GetSendFrames`
与拥塞控制的回归：

```bash
go test ./network/ -run='^$' -bench='BenchmarkFrameMgr|BenchmarkBBCongestion' -benchmem -count=6 -timeout 10m | tee new.txt
# 与之前保存的 old.txt 对比：
benchstat old.txt new.txt
```

`benchstat` 安装：`go install golang.org/x/perf/cmd/benchstat@latest`。

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

## RDNS 弱网回归

`go test -race ./network -run '^TestRdnsWeakNetwork$' -count=3 -timeout=5m`
通过本地无特权的脚本化 UDP 中继，在双向链路中注入丢包、重复、延迟和实际乱序，
并逐字节校验由固定随机种子生成的载荷（每个方向 16 KiB，覆盖 TXT 和 NULL+PSK）。
每次传输的时间上限为 45 秒；失败时会清理中继工作协程和连接读写。
FrameMgr ARQ 可在存活会话内恢复短暂丢包，但不提供自动重连：调用 `Close` 或连接
超时终止后，必须显式重新 `Dial` 创建新会话，原字节流不会续传。回归测试还验证
重新拨号会创建新会话，以及向已关闭的连接写入会失败。

`Protocols` CI 工作流按 tcp/udp/rudp/kcp/quic/rhttp/wss/rdns/ricmp 命名矩阵条目。
RDNS 条目包含弱网回归；WSS 使用本地生成的测试证书。每个条目都要求实际运行并通过
测试，匹配为空或有跳过测试时均失败。原始 ICMP 使用 `CAP_NET_RAW` 运行，覆盖
IPv4/IPv6。完整测试及覆盖率、静态检查、完整竞态检测和大端检查仍由独立任务执行。
