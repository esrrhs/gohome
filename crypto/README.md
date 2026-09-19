# crypto

[中文文档](README_ZH.md)

`crypto` provides a specialized cryptographic suite in Go, featuring a full implementation of the CryptoNight hashing algorithm family alongside underlying cryptographic primitives.

---

## Features

- **CryptoNight Algorithm Family**: Complete implementation supporting all major variants:
  - `cn/0`, `cn/1`, `cn/2`, `cn/r`, `cn/fast`, `cn/half`, `cn/xao`, `cn/rto`, `cn/rwz`, `cn/double`
  - `cn-lite/0`, `cn-lite/1`
  - `cn-heavy/0`, `cn-heavy/tube`, `cn-heavy/xhv`
  - `cn-pico`, `cn-pico/tlo`
- **Underlying Primitives**: High-performance implementations of AES, Blake256, Groestl, JH, RIPEMD160, SHA-3, Skein, and Threefish.

---

## Usage

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/crypto"
)

func main() {
	// Initialize CryptoNight engine
	c := crypto.NewCrypto("cryptonight")

	data := []byte("block header data")
	algo := "cn/1"
	height := uint64(1000000)

	hash := c.Sum(data, algo, height)
	fmt.Printf("Calculated %s hash: %x\n", algo, hash)

	// List all supported algorithm variants
	fmt.Printf("Supported algorithms: %v\n", crypto.Algo())
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
