# loggo

[中文文档](README_ZH.md)

`loggo` is a colorized, daily-rotating logging library in Go, featuring automatic log retention management and built-in panic dump recovery.

---

## Features

- **Level-Based Filtering**: `DEBUG`, `INFO`, `WARN`, `ERROR`.
- **Terminal Color Formatting**: ANSI true-color highlighting (custom RGB text colors).
- **Daily Log File Rotation**: Automatically writes to `{Prefix}_{LEVEL}_{YYYY-MM-DD}.log`.
- **Automatic Retention Cleanup**: Deletes log files older than `MaxDay` days on a continuous background checker.
- **Crash Interceptor**: Intercepts unhandled panics and prints clean, formatted goroutine stack dumps.

---

## Configuration & Usage

```go
package main

import (
	"github.com/esrrhs/gohome/loggo"
)

func main() {
	// Initialize loggo
	loggo.Ini(loggo.Config{
		Prefix:     "myservice", // Log file prefix
		Level:      loggo.LEVEL_DEBUG,
		MaxDay:     7,           // Retain logs for 7 days
		NoLogFile:  false,       // Also write to files
		NoPrint:    false,       // Print to stdout
		NoLogColor: false,       // Use ANSI colors in terminal
		FullPath:   false,       // Show short file basename
	})

	loggo.Debug("Debug info: %v", 42)
	loggo.Info("Application initialized successfully")
	loggo.Warn("Resource usage high: %d%%", 85)
	loggo.Error("Failed to connect to backend: %v", "timeout")
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
