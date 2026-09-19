# common

[English](README.md)

`common` 汇集了一套高频实用的通用基础设施工具函数库，涵盖高效数据压缩、哈希算法、格式化表格、容灾文件读写、DNS 与网络实用工具，以及动态 Protobuf 反射。

---

## 核心功能分类

- **数据压缩 (`alg.go`)**：集成 Zstd（`CompressDataZstd` / `DeCompressDataZstd`）、Zlib 与 Gzip 高性能压缩/解压封装。
- **加解密与算法 (`alg.go`)**：RC4 流加密、UUID 生成、主机字节端序检测（`IsBigEndian`）。
- **哈希函数集 (`hash.go`)**：MD5、XXHash-64、CRC32、FNV-64a 及面向泛型任意类型的 `HashGeneric[T]`。
- **容灾文件操作 (`file.go`)**：
  - `SaveJson` / `LoadJson`：带 `.back` 自动备份镜像，防止写盘中途断电或崩溃导致配置文件损坏。
  - 支持软链接穿透的递归遍历（`Walk`）。
  - 文件 MD5 计算、文本行数统计与原位字符串替换。
- **网络与 DNS 工具 (`net.go`)**：
  - 本机外网出口 IP 获取（`GetOutboundIP`）。
  - 私有内网网段判断（`IsPrivateIP`）。
  - DoH（DNS-over-HTTPS）高速域名解析（`ResolveDomainToIP`）。
  - 根域名提取（`GetRootDomain`，提取 eTLD+1）。
- **字符串与表格 (`string.go`)**：
  - ASCII 字符对齐表格排版渲染（`StrTable`、`StructToTable`）。
  - 36 进制与 62 进制数值转换（`NumToHex`、`Hex2Num`）。
- **动态 Protobuf (`proto.go`)**：
  - 动态载入 DescriptorSet 二进制定义文件。
  - 反射遍历 Service 与 Method RPC 描述符。
  - 动态 Protobuf 结构体与格式化 JSON 互相转换。

---

## 常用代码示例

### 1. 容灾备份的 JSON 存取

```go
type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

cfg := Config{Host: "127.0.0.1", Port: 8080}
// 自动同时写入 config.json 和 config.json.back
_ = common.SaveJson("config.json", &cfg)

var loaded Config
// 当主文件异常损坏时，自动尝试从 .back 恢复
_ = common.LoadJson("config.json", &loaded)
```

### 2. DoH 域名解析与根域名提取

```go
// 通过 DoH 安全解析域名
ip, err := common.ResolveDomainToIP("www.github.com")

// 自动剔除端口并提取根域名，返回 "github.com"
root, err := common.GetRootDomain("sub.domain.github.com:443")
```

### 3. Zstd 快速压缩解压

```go
raw := []byte("需要压缩的大文本数据...")
compressed := common.CompressDataZstd(raw)
decompressed, err := common.DeCompressDataZstd(compressed)
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
