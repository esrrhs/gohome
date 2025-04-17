# GoHome
[<img src="https://img.shields.io/github/license/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/languages/top/esrrhs/gohome">](https://github.com/esrrhs/gohome)
[![Go Report Card](https://goreportcard.com/badge/github.com/esrrhs/gohome)](https://goreportcard.com/report/github.com/esrrhs/gohome)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/gohome/go.yml?branch=master">](https://github.com/esrrhs/gohome/actions)

Go的通用开发库

## 文件夹
### common
* 压缩、解压缩
* channel封装
* 颜色定义
* 错误处理
* 文件操作
* hash函数
* 数学函数
* 网络操作
* protobuf
* 字符串处理
* 时间处理

### crypto
* CryptoNight算法（cn/0，cn/1，cn/2，cn/r，cn/fast，cn/half，cn/xao，cn/rto，cn/rwz，cn/double，cn-lite/0，cn-lite/1，cn-heavy/0，cn-heavy/tube，cn-heavy/xhv，cn-pico，cn-pico/tlo）

### list
* 循环数组
* 有锁链表
* 循环队列
* 请求队列

### loggo
* 日志库
* 终端颜色支持

### lru
* LRU缓存
* LRU资源池

### network
* 抽象网络库（tcp、udp、kcp、rudp、ricmp、rhttp）
* 可靠帧控制
* 拥塞控制
* socks5代理

### platform
* shell调用

### pool
* 对象池
* 令牌桶

### thirdparty
* IP查询
* Mysql自失效KV表

### thread
* 线程池
* 协程组
* 任务池
