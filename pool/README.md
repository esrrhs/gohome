# pool

[中文文档](README_ZH.md)

`pool` provides reusable object allocation pools and channel-based token bucket resource pools to minimize heap allocation and control concurrency in Go.

---

## Features

- **Object Pool (`Pool`)**: Pre-allocates and recycles heavy objects via a custom allocator, tracking in-use and free counts to alleviate garbage collection overhead.
- **Token Pool (`TokenPool`)**: Lightweight channel-based resource/token pool for rate-limiting, slot allocation, and fixed-capacity resource management.

---

## Components & Usage

### 1. `Pool` (Generic Object Recycling)

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/pool"
)

type Buffer struct {
	Data [4096]byte
}

func main() {
	p := pool.New(func() interface{} {
		return &Buffer{}
	})

	elem := p.Alloc()
	buf := elem.Value.(*Buffer)
	buf.Data[0] = 1

	fmt.Printf("Used: %d, Free: %d\n", p.UsedSize(), p.FreeSize())

	p.Free(elem)
	fmt.Printf("After Free - Used: %d, Free: %d\n", p.UsedSize(), p.FreeSize())
}
```

### 2. `TokenPool` (Resource Slot Management)

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/pool"
)

func main() {
	// Create a pool with 10 concurrency tokens/slots
	tp := pool.NewTokenPool(10)

	// Acquire a slot (blocks if no tokens are available)
	token := tp.Acquire()
	fmt.Printf("Acquired token slot: %d\n", token)

	// Release slot back to pool
	tp.Release(token)
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
