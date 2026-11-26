package common

import (
	"errors"
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"runtime"
)

func CrashLog() {
	if r := recover(); r != nil {
		var err error
		switch x := r.(type) {
		case string:
			err = errors.New(x)
		case error:
			err = x
		default:
			err = errors.New("Unknown panic")
		}
		if err != nil {
			loggo.Error("crash %s \n%s", err, DumpStacks())
		}
		panic(err)
	}
}

func DumpStacks() string {
	size := 16384
	const maxStackSize = 4 << 20
	for {
		buf := make([]byte, size)
		n := runtime.Stack(buf, true)
		if n < size || size >= maxStackSize {
			return fmt.Sprintf("=== BEGIN goroutine stack dump ===\n%s\n=== END goroutine stack dump ===", buf[:n])
		}
		size *= 2
	}
}
