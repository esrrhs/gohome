# loggo

[English](README.md)

`loggo` 是一个支持彩色终端输出、按天自动切割并定期清理的 Go 语言轻量级日志库，内置 Panic 崩溃堆栈自动捕获功能。

---

## 核心特性

- **多日志级别**：支持 `DEBUG`、`INFO`、`WARN`、`ERROR`。
- **真彩色终端输出**：支持 ANSI RGB 前景色格式化高亮。
- **自动按天轮转写盘**：自动归档为 `{Prefix}_{LEVEL}_{YYYY-MM-DD}.log` 格式。
- **过期自动清理**：后台自动检测并删除超过 `MaxDay` 天的旧日志文件。
- **崩溃堆栈捕获**：提供统一的 Panic 恢复与格式化 Goroutine 完整堆栈转储输出。

---

## 配置与使用示例

```go
package main

import (
	"github.com/esrrhs/gohome/loggo"
)

func main() {
	// 初始化配置
	loggo.Ini(loggo.Config{
		Prefix:     "myservice", // 日志文件名前缀
		Level:      loggo.LEVEL_DEBUG,
		MaxDay:     7,           // 最多保留 7 天日志
		NoLogFile:  false,       // 同步写入日志文件
		NoPrint:    false,       // 终端输出
		NoLogColor: false,       // 终端开启彩色高亮
		FullPath:   false,       // 仅显示文件名，不显示绝对全路径
	})

	loggo.Debug("调试信息: %v", 42)
	loggo.Info("服务初始化完成")
	loggo.Warn("系统资源负载偏高: %d%%", 85)
	loggo.Error("连接后端服务失败: %v", "timeout")
}
```

---

## 开源协议

本项目属于 [GoHome](https://github.com/esrrhs/gohome)，遵循 [MIT 开源许可证](../LICENSE)。
