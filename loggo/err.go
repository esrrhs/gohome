//go:build !linux && !darwin

package loggo

import (
	"os"
)

func rewriteStderrFile() {
	stdErrFile := gConfig.Prefix + ".stderr"
	file, err := os.OpenFile(stdErrFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}

	// 将标准错误输出重定向到文件
	os.Stderr = file
}
