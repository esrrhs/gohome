# platform

[中文文档](README_ZH.md)

`platform` provides cross-platform command execution, shell script runners, and external process management with timeouts and integrated logging.

---

## Features

- **Script Runner (`ShellRun` / `ShellRunTimeout`)**: Runs shell scripts (`sh script.sh ...`) with parameter forwarding and deadline contexts.
- **Raw Command Runner (`ShellRunCommand`)**: Executes commands through bash (`bash -c "..."`) and captures combined stdout/stderr.
- **Executable Runner (`ShellRunExe` / `ShellRunExeTimeout` / `ShellRunExeRaw`)**: Directly spawns binary executables with logging and execution time tracking.

---

## Usage

```go
package main

import (
	"fmt"
	"github.com/esrrhs/gohome/platform"
)

func main() {
	// 1. Run raw shell command
	out, err := platform.ShellRunCommand("echo 'hello world'", false)
	if err == nil {
		fmt.Println("Output:", out)
	}

	// 2. Run script with a 5-second timeout
	out, err = platform.ShellRunTimeout("./deploy.sh", false, 5, "--env=prod")

	// 3. Run standalone executable binary
	out, err = platform.ShellRunExe("git", false, "status")
}
```

---

## License

This package is part of the [GoHome](https://github.com/esrrhs/gohome) project under the [MIT License](../LICENSE).
