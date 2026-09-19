# crypto

[English](README.md)

`crypto` 提供了加密与散列计算套件，重点实现了完整的 CryptoNight 系列工作量证明散列算法及其底层密码学组件。

---

## 核心特性

- **CryptoNight 全系列算法支持**：
  - 标准系列：`cn/0`, `cn/1`, `cn/2`, `cn/r`, `cn/fast`, `cn/half`, `cn/xao`, `cn/rto`, `cn/rwz`, `cn/double`
  - 轻量级系列：`cn-lite/0`, `cn-lite/1`
  - 重量级系列：`cn-heavy/0`, `cn-heavy/tube`, `cn-heavy/xhv`
  - 极速系列：`cn-pico`, `cn-pico/tlo`
- **底层密码学原语**：内置针对 AES、Blake256、Groestl、JH、RIPEMD160、SHA-3、Skein 及 Threefish 的底层高效散列计算实现。

---

## 使用示例

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/crypto"
)

func main() {
	// 初始化 CryptoNight 引擎
	c := crypto.NewCrypto("cryptonight")

	data := []byte("区块头原始数据")
	algo := "cn/1"
	height := uint64(1000000)

	hash := c.Sum(data, algo, height)
	fmt.Printf("计算 %s 哈希结果: %x\n", algo, hash)

	// 列出所有支持的算法版本
	fmt.Printf("支持的算法列表: %v\n", crypto.Algo())
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
