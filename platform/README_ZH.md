# platform

[English](README.md)

`platform` 封装了跨平台的命令行执行、Shell 脚本运行与独立进程调度功能，内置执行超时上下文控制与执行耗时日志。

---

## 核心特性

- **Shell 脚本执行 (`ShellRun` / `ShellRunTimeout`)**：调用 `sh` 执行脚本文件，支持参数透传与硬超时控制。
- **原生命令执行 (`ShellRunCommand`)**：通过 `bash -c` 执行复杂的 Shell 命令并合并捕获标准输出与标准错误。
- **可执行文件拉起 (`ShellRunExe` / `ShellRunExeTimeout` / `ShellRunExeRaw`)**：直接拉起二进制程序并支持耗时追踪与无侵入输出获取。

---

## 使用示例

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/platform"
)

func main() {
	// 1. 执行 Shell 管道/命令
	out, err := platform.ShellRunCommand("echo 'hello world'", false)
	if err == nil {
		fmt.Println("输出:", out)
	}

	// 2. 带 5 秒超时执行脚本
	out, err = platform.ShellRunTimeout("./deploy.sh", false, 5, "--env=prod")

	// 3. 拉起独立二进制程序
	out, err = platform.ShellRunExe("git", false, "status")
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
