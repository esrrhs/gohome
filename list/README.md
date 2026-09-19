# list

[中文文档](README_ZH.md)

`list` provides specialized, high-performance data structures including thread-safe byte ring buffers, indexed circular buffers, fixed-size ring queues, single-flight request deduplication queues, and thread-safe linked lists.

---

## Features

- **`RBuffergo`**: Thread-safe circular byte ring buffer with slice bookmarking and read/write pointer management.
- **`ROBuffergo`**: Ring buffer indexing elements by unique IDs, ideal for windowed sequencing and sliding protocol frames.
- **`Rlistgo`**: Fixed-capacity ring queue for generic interface elements.
- **`ReqQueue[K, V]`**: Single-flight concurrent request deduplication queue (anti-stampede pattern).
- **`synclist`**: Mutex-synchronized doubly-linked list wrapping standard `container/list`.

---

## Components & Usage

### 1. `RBuffergo` (Byte Ring Buffer)

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/list"
)

func main() {
	// 1024-byte capacity, thread-safe with internal mutex
	rb := list.NewRBuffergo(1024, true)

	rb.Write([]byte("hello world"))
	fmt.Printf("Buffer length: %d, free space: %d\n", rb.GetSize(), rb.GetLeft())

	out := make([]byte, 5)
	rb.Read(out)
	fmt.Printf("Read: %s\n", string(out))
}
```

### 2. `ReqQueue[K, V]` (Request Deduplication / Single-Flight)

When multiple callers request the same key concurrently, only the first executes the callback while others wait and share the result.

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/list"
)

func main() {
	rq := list.NewReqQueue(func(key string) (string, error) {
		time.Sleep(100 * time.Millisecond) // Simulate slow query
		return "data for " + key, nil
	})

	// Concurrent callers for the same key
	go func() {
		val, _ := rq.Submit("user_42")
		fmt.Println("Goroutine 1:", val)
	}()

	val, _ := rq.Submit("user_42")
	fmt.Println("Goroutine 2:", val)
}
```

### 3. `synclist` (Thread-Safe Linked List)

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

	fmt.Printf("Size: %d, Popped: %v\n", l.Len(), l.Pop())
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
