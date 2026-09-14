# GoHome

[<img src="https://img.shields.io/github/license/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/languages/top/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/gohome/go.yml?branch=master">](https://github.com/esrrhs/gohome/actions)
[<img src="https://img.shields.io/badge/go-%3E%3D1.24-blue">](https://golang.org/)

[English](README.md)

GoHome 是一个功能完备、开箱即用的 Go 语言通用基础设施与核心算法库。提供跨多种可靠与不可靠协议的高性能抽象网络层、拥塞控制、并发协同模型、多级 LRU 缓存、加密算法套件以及丰富的通用工具函数集。

---

## 目录

- [功能特性](#功能特性)
- [模块概览](#模块概览)
  - [network - 网络抽象与协议库](#network---网络抽象与协议库)
  - [lru - 高并发 LRU 缓存系统](#lru---高并发-lru-缓存系统)
  - [thread - 并发与协程管理](#thread---并发与协程管理)
  - [pool - 对象与令牌池](#pool---对象与令牌池)
  - [list - 数据结构与缓冲队列](#list---数据结构与缓冲队列)
  - [crypto - 加密套件](#crypto---加密套件)
  - [loggo - 彩色日志库](#loggo---彩色日志库)
  - [common - 常用基础设施工具集](#common---常用基础设施工具集)
  - [platform - 跨平台命令行与脚本执行](#platform---跨平台命令行与脚本执行)
  - [thirdparty - 第三方集成](#thirdparty---第三方集成)
- [快速开始](#快速开始)
  - [安装依赖](#安装依赖)
  - [网络连接抽象示例](#网络连接抽象示例)
  - [多层级 LRU 缓存示例](#多层级-lru-缓存示例)
  - [层级协程管理示例](#层级协程管理示例)
- [开源协议](#开源协议)

---

## 功能特性

- **多协议统一抽象**：通过统一的 `Conn` 接口无缝切换 TCP、UDP、KCP、QUIC、RUDP、RICMP 及 RHTTP。
- **可靠传输控制**：内置基于滑动窗口与分帧控制的 Reliable Frame Control，以及基于带宽探测的自适应拥塞控制算法（BBCongestion）。
- **高性能缓存机制**：提供基于泛型的基础 LRU 缓存、多分片低锁竞争的 `LRUMultiCache` 以及带请求合并队列的 `LRUResourceCache`。
- **结构化并发管理**：具备层级父子关系的协程组管理（`Group`）、多 Worker 任务池（`TaskPool`）与工作线程池（`ThreadPool`）。
- **专业加密算法**：内置完整适配的 CryptoNight 全系列算法实现及 AES、Blake256、Groestl、JH、RIPEMD160、Skein 等哈希组件。
- **高复用工具箱**：涵盖 JSON 带备份容灾存取、DNS-over-HTTPS (DoH) 解析、根域名提取、Zlib/Gzip 压缩、RC4 加密及端序检测等。

---

## 模块概览

### network - 网络抽象与协议库
- **统一连接接口 (`Conn`)**：标准定义 `Dial`、`Listen`、`Accept` 及 `ReadWriteCloser`，抹平协议差异。
- **支持传输协议**：
  - `tcp`：标准 TCP 封装。
  - `udp`：无连接与会话模拟 UDP 传输。
  - `kcp`：基于 ARQ 的低延迟可靠传输。
  - `quic`：基于 QUIC 协议多路复用连接。
  - `rudp`：自定义轻量级可靠 UDP 实现。
  - `ricmp`：基于 ICMP 报文构建的隐蔽可靠信道。
  - `rhttp`：基于 HTTP 协议通道的可靠穿透通信。
- **帧管理 (`FrameMgr`)**：数据帧序号控制、超时重传、滑动窗口机制与状态探活心跳。
- **拥塞控制 (`BBCongestion`)**：基于实际带宽与飞行数据量估算并动态调整窗口大小的自适应拥塞算法。
- **SOCKS5 代理**：轻量级 SOCKS5 客户端及服务端握手与请求转发支持。

### lru - 高并发 LRU 缓存系统
- **`LRUCache[K, V]`**：基于 Go 泛型和双向链表+哈希表的经典 LRU 缓存，支持全局 TTL 过期淘汰。
- **`LRUMultiCache[K, V]`**：根据键哈希自动分片至多个独立 LRU 实例，大幅降低多核高并发访问下的锁竞争。
- **`LRUResourceCache[K, V]`**：集成 `ReqQueue`，当高并发穿透未命中缓存时自动进行并发合并与异步请求复用。

### thread - 并发与协程管理
- **`Group`**：支持树状父子关系的 Goroutine 管理，具备优雅退出、错误向上冒泡以及 Panic 恢复能力。
- **`TaskPool`**：适用于 CPU 密集型任务的并发处理池，支持批量任务分发及完成状态同步。
- **`ThreadPool`**：多通道队列绑定的并发 Worker 线程池，具备状态监控与数据负载统计能力。

### pool - 对象与令牌池
- **`Pool`**：通用对象池，预分配并复用开销较大的对象，监控占用及空闲数量，降低 GC 压力。
- **`TokenPool`**：基于 Channel 构建的令牌池与并发限流资源池，支持阻塞申请与归还。

### list - 数据结构与缓冲队列
- **`RBuffergo`**：高性能并发安全字节循环环形缓冲区（Ring Buffer）。
- **`ROBuffergo`**：带唯一 ID 索引与标记的环形数据缓冲区。
- **`Rlistgo`**：固定容量的环形队列。
- **`ReqQueue`**：同名/同 Key 异步任务请求合并防击穿队列。
- **`synclist`**：线程安全的双向链表。

### crypto - 加密套件
- **CryptoNight**：全面支持 CryptoNight 家族算法（包括 cn/0, cn/1, cn/2, cn/r, cn/fast, cn/half, cn/xao, cn/rto, cn/rwz, cn/double, cn-lite 系列, cn-heavy 系列, cn-pico 系列等）。
- **底层密码学组件**：集成了 AES、Blake256、Groestl、JH、RIPEMD160、SHA-3、Skein 及 Threefish 等底层密码学散列计算。

### loggo - 彩色日志库
- 支持 `DEBUG`、`INFO`、`WARN`、`ERROR` 多日志级别过滤。
- 终端 ANSI 真彩色输出与控制。
- 自动按天切割日志文件并根据保留期限（`MaxDay`）自动清理过期日志。
- 支持 Crash/Panic 自动捕获与完整堆栈回溯输出。

### common - 常用基础设施工具集
- **数据压缩**：Zlib / Gzip 快速压缩与解压缩。
- **算法加解密**：RC4 加解密、通用 UUID 生成、端序判断（Big Endian / Little Endian）。
- **数学与随机**：类型安全数值计算（`Min`、`Max`、`Abs`、`SafeDivide`）、随机数生成与切片打乱（`Shuffle`）。
- **哈希函数**：MD5、XXHash、CRC32、FNV-64a 及面向泛型类型的快速哈希 `HashGeneric[T]`。
- **文件操作**：带 `.back` 自动防损的 JSON 容灾读写、递归 Symlink 遍历（`Walk`）、MD5 校验、文本行统计与批量替换。
- **网络与 DNS 工具**：外网出口 IP 获取、私有内网 IP 判断、DoH（DNS-over-HTTPS）域名解析及 eTLD+1 根域名提取。
- **Protobuf**：动态加载 DescriptorSet、反射导出结构以及序列化为完整 JSON。

### platform - 跨平台命令行与脚本执行
- **`ShellRun` / `ShellRunTimeout`**：跨平台脚本执行与超时控制。
- **`ShellRunCommand`**：执行原生 Shell 命令并捕获合并标准输出与标准错误。
- **`ShellRunExe` / `ShellRunExeTimeout`**：二进制可执行程序拉起与执行跟踪。

### thirdparty - 第三方集成
- **`GeoIP2`**：封装 MaxMind GeoLite2 数据库，支持离线高速解析 IP 所属国家与 ISO 代码。
- **`TMysql`**：轻量级 MySQL 访问封装与自动过期数据淘汰支持。

---

## 快速开始

### 安装依赖

```bash
go get -u github.com/esrrhs/gohome
```

### 网络连接抽象示例

使用统一的 `NewConn` 即可创建不同协议的客户端与服务端：

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	// 支持: "tcp", "udp", "rudp", "ricmp", "kcp", "quic", "rhttp"
	listener, err := network.NewConn("kcp")
	if err != nil {
		panic(err)
	}

	l, err := listener.Listen("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer l.Close()

	// 服务端处理连接
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		fmt.Printf("收到消息: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// 客户端发起连接
	client, _ := network.NewConn("kcp")
	c, err := client.Dial("127.0.0.1:8888")
	if err != nil {
		panic(err)
	}
	defer c.Close()

	c.Write([]byte("ping"))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	fmt.Printf("客户端响应: %s\n", string(buf[:n]))
}
```

### 多层级 LRU 缓存示例

利用 `LRUMultiCache` 消除高并发读写热点锁竞争：

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/lru"
)

func main() {
	// 创建分片数=8, 容量=1000, TTL=10分钟的缓存
	cache := lru.NewLRUMultiCache[string, int](8, 1000, 10*time.Minute)

	cache.Set("user_1001", 99)

	if val, ok := cache.Get("user_1001"); ok {
		fmt.Printf("获取到缓存值: %d\n", val)
	}
}
```

### 层级协程管理示例

使用 `thread.Group` 安全管理子 Goroutine 生命周期：

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/thread"
)

func main() {
	rootGroup := thread.NewGroup("root", nil, func() {
		fmt.Println("根协程组退出通知")
	})

	// 启动子工作任务
	rootGroup.Go(func() {
		fmt.Println("任务处理中...")
		time.Sleep(100 * time.Millisecond)
	})

	// 等待退出
	rootGroup.Exit()
	rootGroup.Wait()
}
```

---

## 开源协议

本项目采用 [MIT 许可证](LICENSE)。
