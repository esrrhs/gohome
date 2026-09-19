# common

[中文文档](README_ZH.md)

`common` provides a comprehensive collection of general-purpose utility functions, covering data compression, cryptographic hashing, string tables, filesystem operations, DNS and networking tools, and dynamic Protobuf introspection.

---

## Key Feature Groups

- **Compression (`alg.go`)**: Zstd (`CompressDataZstd` / `DeCompressDataZstd`), Zlib, and Gzip compression/decompression helpers.
- **Security & Ciphers (`alg.go`)**: RC4 stream cipher, UUID generation, native system endianness detection (`IsBigEndian`).
- **Hashing (`hash.go`)**: MD5, XXHash-64, CRC32, FNV-64a, and generic type hasher `HashGeneric[T]`.
- **Fault-Tolerant Filesystem (`file.go`)**:
  - `SaveJson` / `LoadJson` with automatic `.back` backup mirroring to prevent corruptions during power failures or process terminations.
  - Symlink-aware recursive traversal (`Walk`).
  - File MD5 calculation, line count, and in-place string replacement.
- **Networking & DNS (`net.go`)**:
  - Outbound IP detection (`GetOutboundIP`).
  - Private IP check (`IsPrivateIP`).
  - DoH (DNS-over-HTTPS) query resolving (`ResolveDomainToIP`).
  - Root domain parser (`GetRootDomain`, extracting eTLD+1).
- **String Formatting & Tables (`string.go`)**:
  - ASCII formatted tabular rendering (`StrTable`, `StructToTable`).
  - Base-36 / Base-62 numeric base conversion (`NumToHex`, `Hex2Num`).
- **Dynamic Protobuf (`proto.go`)**:
  - Dynamic loading of `.pb` `FileDescriptorSet`.
  - Introspection of service RPC methods.
  - Reflection-based serialization of dynamic messages to indented JSON.

---

## Code Examples

### 1. Fault-Tolerant JSON Persistence

```go
type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

cfg := Config{Host: "127.0.0.1", Port: 8080}
// Saves to both config.json and config.json.back
_ = common.SaveJson("config.json", &cfg)

var loaded Config
// Automatically falls back to .back file if main file is corrupted
_ = common.LoadJson("config.json", &loaded)
```

### 2. DNS-over-HTTPS & Root Domain Extraction

```go
// Resolves domain via DoH (AliDNS)
ip, err := common.ResolveDomainToIP("www.github.com")

// Extracts eTLD+1: returns "github.com"
root, err := common.GetRootDomain("sub.domain.github.com:443")
```

### 3. Zstd Compression

```go
raw := []byte("repeated payload ...")
compressed := common.CompressDataZstd(raw)
decompressed, err := common.DeCompressDataZstd(compressed)
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
