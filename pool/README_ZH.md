# pool

[English](README.md)

`pool` 提供了通用对象复用池与基于 Channel 构建的令牌资源池，用于降低频繁堆内存分配带来的 GC 压力并实现精准的并发限制。

---

## 核心特性

- **对象复用池 (`Pool`)**：允许用户自定义对象分配逻辑，自动追踪处于使用中和空闲的对象数量，有效降低对象创建和垃圾回收开销。
- **令牌/资源池 (`TokenPool`)**：利用 Go Channel 特性实现的轻量级定长资源槽池，提供并发限流、阻塞申请与安全归还机制。

---

## 组件说明与用法

### 1. 通用对象池：`Pool`

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

	fmt.Printf("已用: %d, 空闲: %d\n", p.UsedSize(), p.FreeSize())

	p.Free(elem)
	fmt.Printf("释放后 - 已用: %d, 空闲: %d\n", p.UsedSize(), p.FreeSize())
}
```

### 2. 令牌与槽位资源池：`TokenPool`

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/pool"
)

func main() {
	// 创建最大容量为 10 的并发令牌池
	tp := pool.NewTokenPool(10)

	// 申请令牌（无空闲时自动阻塞等待）
	slot := tp.Acquire()
	fmt.Printf("获取到槽位: %d\n", slot)

	// 使用完毕归还槽位
	tp.Release(slot)
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
