# thread

[English](README.md)

`thread` 提供了 Go 语言的结构化并发协同机制、树状协程生命周期管控工具、批量任务执行池以及多队列 Worker 线程池。

---

## 核心特性

- **层级协程管理 (`Group`)**：支持树状父子关系的 Goroutine 管理，具备级联退出通知、Panic 自动恢复、错误向上传播，以及通过 `Join()` 实现无竞态的优雅同步。
- **CPU 密集任务池 (`TaskPool`)**：针对批量计算型任务优化的多 Worker 任务池，支持异步分发与同步等待完成通知。
- **多通道工作线程池 (`ThreadPool`)**：固定并发度的 Worker 线程池，支持独立 Channel 队列隔离、超时写入，以及任务负载与处理统计。

---

## 组件说明与用法

### 1. 树状协程管理器：`Group`

将 Goroutine 组织成树状层级结构。当父 Group 退出或出错时，子 Goroutine 会协同响应并安全退出，避免协程泄漏。

```go
package main

import (
	"fmt"
	"time"
	"github.com/esrrhs/gohome/thread"
)

func main() {
	root := thread.NewGroup("root", nil, func() {
		fmt.Println("根协程组退出回调")
	})

	// 在 root 下启动子协程
	root.Go("worker-1", func() error {
		for !root.IsExit() {
			time.Sleep(50 * time.Millisecond)
		}
		return nil
	})

	time.Sleep(100 * time.Millisecond)
	root.Stop()
	_ = root.Join() // 等待所有子协程彻底执行完毕退出
}
```

### 2. CPU 任务处理池：`TaskPool`

将任务并发分发给固定数量的后台 Worker 执行：

```go
// 4 个 Worker，任务缓冲队列长度 100
pool := thread.NewTaskPool(4, 100)

task := pool.AddTask(func() {
    // 耗时计算逻辑
})

// 可选：等待该任务执行完成
task.Wait()

pool.Stop()
```

### 3. 多队列线程池：`ThreadPool`

```go
// 4 个通道并发处理，每个通道缓冲 50
tp := thread.NewThreadPool(4, 50, func(item interface{}) {
    fmt.Printf("处理任务: %v\n", item)
})

tp.Push("job-data")
tp.Stop()
tp.Wait()
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
