# thread

[中文文档](README_ZH.md)

`thread` provides structured concurrency, hierarchical goroutine lifecycle coordination, task batching pools, and channel-bound worker thread pools for Go applications.

---

## Features

- **Hierarchical Concurrency (`Group`)**: Tree-structured goroutine management with cascading exit propagation, panic recovery, error bubbling, and clean synchronization via `Join()`.
- **CPU-Bound Task Pool (`TaskPool`)**: Worker pool optimized for batch CPU-intensive jobs with asynchronous submission and completion notification.
- **Worker Thread Pool (`ThreadPool`)**: Fixed-size worker pool with channel queue isolation, task push timeouts, and operational throughput/depth metrics.

---

## Components & Usage

### 1. `Group` (Hierarchical Goroutines)

Manages goroutines as trees. If a parent exits or errors, child goroutines are notified cooperatively to terminate gracefully.

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/thread"
)

func main() {
	root := thread.NewGroup("root", nil, func() {
		fmt.Println("Root teardown callback")
	})

	// Spawn a worker under root
	root.Go("worker-1", func() error {
		for !root.IsExit() {
			time.Sleep(50 * time.Millisecond)
		}
		return nil
	})

	time.Sleep(100 * time.Millisecond)
	root.Stop()
	_ = root.Join() // Wait cleanly for all children to exit
}
```

### 2. `TaskPool` (CPU Task Processing)

Distributes tasks among a fixed set of background worker goroutines.

```go
// 4 workers, task queue buffer size 100
pool := thread.NewTaskPool(4, 100)

task := pool.AddTask(func() {
    // CPU-intensive computation
})

// Optionally block until this task finishes
task.Wait()

pool.Stop()
```

### 3. `ThreadPool` (Channel-Backed Worker Pool)

```go
// 4 worker channels, queue buffer size 50
tp := thread.NewThreadPool(4, 50, func(item interface{}) {
    fmt.Printf("Processing item: %v\n", item)
})

tp.Push(123)
tp.Stop()
tp.Wait()
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
