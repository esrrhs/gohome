# list

[English](README.md)

`list` 提供了一组专用、高性能的数据结构，包括线程安全的字节环形缓冲区、带 ID 索引的环形缓冲、定长环形队列、并发单飞请求合并防击穿队列以及线程安全的双向链表。

---

## 核心特性

- **`RBuffergo`**：高性能并发安全字节循环环形缓冲区（Ring Buffer），支持读写指针管理与状态暂存恢复。
- **`ROBuffergo`**：带唯一 ID 索引与标记的环形数据缓冲区，专为滑动窗口协议帧管理设计。
- **`Rlistgo`**：固定容量的通用元素环形队列。
- **`ReqQueue[K, V]`**：并发异步任务请求合并队列（Single-Flight 模式），防止同 Key 瞬时高并发请求击穿。
- **`synclist`**：互斥锁保护的并发安全双向链表（封装标准库 `container/list`）。

---

## 组件说明与用法

### 1. 字节环形缓冲区：`RBuffergo`

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/list"
)

func main() {
	// 容量 1024 字节，开启内部互斥锁保证线程安全
	rb := list.NewRBuffergo(1024, true)

	rb.Write([]byte("hello world"))
	fmt.Printf("当前数据长度: %d, 剩余可用空间: %d\n", rb.GetSize(), rb.GetLeft())

	out := make([]byte, 5)
	rb.Read(out)
	fmt.Printf("读取数据: %s\n", string(out))
}
```

### 2. 请求合并队列：`ReqQueue[K, V]`

当多个并发 Goroutine 同时请求相同的 Key 时，只有第一个调用真正执行加载函数，其余调用挂起等待并共享该结果。

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/list"
)

func main() {
	rq := list.NewReqQueue(func(key string) (string, error) {
		time.Sleep(100 * time.Millisecond) // 模拟慢速外部查询
		return "data for " + key, nil
	})

	// 并发请求同一个 Key
	go func() {
		val, _ := rq.Submit("user_42")
		fmt.Println("Goroutine 1 结果:", val)
	}()

	val, _ := rq.Submit("user_42")
	fmt.Println("Goroutine 2 结果:", val)
}
```

### 3. 并发安全双向链表：`synclist`

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/list"
)

func main() {
	l := list.NewList()
	l.Push("item1")
	l.Push("item2")

	fmt.Printf("长度: %d, 出队元素: %v\n", l.Len(), l.Pop())
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
