package loggo

import (
	"os"
	"runtime"
	"syscall"
)

var stdErrFileHandler *os.File

func rewriteStderrFile() {
	stdErrFile := gConfig.Prefix + ".stderr"
	file, err := os.OpenFile(stdErrFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	stdErrFileHandler = file //把文件句柄保存到全局变量，避免被GC回收

	if err = syscall.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		panic(err)
	}
	// 内存回收前关闭文件描述符
	runtime.SetFinalizer(stdErrFileHandler, func(fd *os.File) {
		fd.Close()
	})
}
