# GoHome

[<img src="https://img.shields.io/github/license/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/languages/top/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/gohome/go.yml?branch=master">](https://github.com/esrrhs/gohome/actions)
[<img src="https://img.shields.io/badge/go-%3E%3D1.24-blue">](https://golang.org/)

[English](README.md)

GoHome 是一个功能完备、开箱即用的 Go 语言通用基础设施与核心算法库。提供跨多种可靠与不可靠协议的高性能抽象网络层、拥塞控制、并发协同模型、多级 LRU 缓存、加密算法套件以及丰富的工程工具函数集。

---

## 子模块概览

每个子模块均包含独立、详细的文档。点击下方模块名称即可跳转至对应的子模块说明，查看详细设计与完整用法：

| 模块名称 | 核心功能简介 | 文档链接 |
|---|---|---|
| **[`network`](network/)** | 统一多协议连接抽象（`Conn` 支持 TCP、UDP、KCP、QUIC、RUDP、RICMP、RHTTP）、滑动窗口分帧管理（`FrameMgr`）、自适应拥塞控制（`BBCongestion`）与标准 SOCKS5 代理。 | [中文](network/README_ZH.md) \| [English](network/README.md) |
| **[`lru`](lru/)** | 基于泛型的经典 LRU 缓存、低锁竞争多分片 `LRUMultiCache`，以及带请求合并防击穿的资源缓存 `LRUResourceCache`。 | [中文](lru/README_ZH.md) \| [English](lru/README.md) |
| **[`thread`](thread/)** | 支持级联取消的树状父子协程组（`Group`）、CPU 密集型批量任务池（`TaskPool`），以及带负载统计的多通道线程池（`ThreadPool`）。 | [中文](thread/README_ZH.md) \| [English](thread/README.md) |
| **[`pool`](pool/)** | 降低 GC 压力的通用对象复用池（`Pool`），以及基于通道构建的并发限流与槽位令牌池（`TokenPool`）。 | [中文](pool/README_ZH.md) \| [English](pool/README.md) |
| **[`list`](list/)** | 线程安全字节环形缓冲（`RBuffergo`）、带 ID 索引环形缓冲（`ROBuffergo`）、环形队列（`Rlistgo`）、单飞请求合并队列（`ReqQueue`）与并发链表（`synclist`）。 | [中文](list/README_ZH.md) \| [English](list/README.md) |
| **[`crypto`](crypto/)** | 完整的 CryptoNight 散列算法家族（全变种 cn/0、cn/1、cn/2、cn/r、cn-lite、cn-heavy、cn-pico 等）以及底层密码学组件（AES、Blake256、Groestl、JH、RIPEMD160、Skein）。 | [中文](crypto/README_ZH.md) \| [English](crypto/README.md) |
| **[`loggo`](loggo/)** | 支持 ANSI 真彩色终端输出的多级别日志库，具备按天自动轮转写盘、基于保留期（`MaxDay`）自动清理，以及 Panic 崩溃堆栈自动转储。 | [中文](loggo/README_ZH.md) \| [English](loggo/README.md) |
| **[`common`](common/)** | 常用基础设施工具集：Zstd/Zlib/Gzip 压缩、RC4 加密、通用类型哈希（`HashGeneric`）、带 `.back` 容灾的 JSON 读写、DoH 解析、根域名提取（eTLD+1）以及动态 Protobuf。 | [中文](common/README_ZH.md) \| [English](common/README.md) |
| **[`platform`](platform/)** | 跨平台命令执行与进程管理：Shell 命令执行（`ShellRunCommand`）、带硬超时控制的脚本执行（`ShellRunTimeout`）与独立二进制程序调度（`ShellRunExe`）。 | [中文](platform/README_ZH.md) \| [English](platform/README.md) |
| **[`thirdparty`](thirdparty/)** | 第三方库适配器：基于 MaxMind GeoLite2 的离线 IP 所属国家查询，以及带自动过期淘汰策略的 MySQL 数据表封装。 | [中文](thirdparty/README_ZH.md) \| [English](thirdparty/README.md) |

---

## 快速开始

### 安装依赖

```bash
go get -u github.com/esrrhs/gohome
```

### 快速示例：统一多协议连接

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/network"
)

func main() {
	// 可随时切换任意协议: "tcp", "udp", "kcp", "quic", "rudp", "ricmp", "rhttp"
	proto := "kcp"

	// 1. 服务端监听
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
		fmt.Printf("服务端接收: %s\n", string(buf[:n]))
		conn.Write([]byte("pong"))
	}()

	// 2. 客户端连接
	clientDriver, _ := network.NewConn(proto)
	c, err := clientDriver.Dial("127.0.0.1:8888")
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

如需查阅具体每个子模块的详细 API、设计背景与完整示例，请点击上方表格中的子模块文档链接直接跳转。

---

## 开源协议

本项目采用 [MIT 许可证](LICENSE)。
