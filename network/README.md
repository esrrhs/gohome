# network

统一网络抽象层（`Conn`），支持 TCP / UDP / RUDP / RICMP / KCP / QUIC / RHTTP。

## 可靠协议下载速度 Benchmark

按顺序测 **rudp → kcp → quic → rhttp → ricmp** 单连接下载吞吐（MB/s）。

### 测法

1. 起一个 server、一个 client
2. server 不停 `Write` **同一块 1024 字节**（各协议载荷完全相同）
3. client 不停 `Read`，凑满 1024 后校验内容是否一致，只累计校验通过的字节
4. 跑满 **1 分钟** 后停
5. `MB/s = 校验通过字节 / 60 / (1024*1024)`

`FrameMgr` 是 rudp/ricmp 内部组件，不单独对比。  
未纳入：裸 `tcp`（未单列）。ricmp 需 `CAP_NET_RAW`。

### 模拟链路

| 参数 | 值 |
|------|-----|
| 连接数 | 1 |
| 单向延迟 | 200ms（RTT ≈ 400ms） |
| 丢包 | 0% / 10% / 50% |
| 载荷 | 固定 1024 字节（内容校验） |
| 时长 | 60s |
| 代理 | rudp/kcp/quic：用户态 **UDP** netem；rhttp：内核 **tc netem**（TCP 端口过滤）；ricmp：内核 **tc netem**（ICMP protocol 过滤） |

依赖：`iproute-tc`、`kernel-modules-extra`（`sch_netem`），需 root。tc netem `limit 100000`（避免默认 1000 队列在高压下丢包）。

### 跑法

```bash
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput -benchtime=1x -timeout 90m -benchmem
# 只跑 ricmp / rhttp：
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/ricmp -benchtime=1x -timeout 30m
go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput/rhttp -benchtime=1x -timeout 30m
```

### 测试环境

| 项 | 值 |
|----|----|
| OS / Arch | linux / amd64 |
| CPU | AMD EPYC 7K62 48-Core Processor |
| 日期 | 2026-09-19 |

### 结果：下载速度（MB/s）

| 协议 | 0% 丢包 | 10% 丢包 | 50% 丢包 |
|------|--------:|--------:|--------:|
| **rudp** | 4.05 | 2.47 | 0.54 |
| **kcp** | 7.39 | 5.13 | 1.83 |
| **quic** | 0.08 | 0.02 | 0.00 |
| **rhttp** | 0.08 | 0.06 | *Skip* |
| **ricmp** | 5.07 | 2.51 | 0.00 |

\* **rhttp @ 50%**：短连接 HTTP + 高丢包握手失败，`Skip`。  
\* **ricmp**：与 rudp 同为 FrameMgr；下行可 ICMP 直推；链路为内核 tc netem。  
\* UDP 三者为用户态 netem；rhttp/ricmp 为内核 tc netem。

### 分析结论

1. **无丢包**：kcp 最快；ricmp（5.07）与 rudp（4.05）同量级；rhttp/quic 较慢。  
2. **10% 丢包**：kcp 仍领先；ricmp ≈ rudp（约 2.5）。  
3. **50% 丢包**：kcp 最好；ricmp 几乎不可用（0.00）；rhttp Skip。  
4. ricmp 与 rhttp 不同：不必靠 client 轮询拉流，server 可主动 ICMP 推送。
