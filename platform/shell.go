package platform

import (
	"context"
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"os/exec"
	"path/filepath"
	"time"
)

/*
shell 提供了一系列用于执行外部命令和脚本的函数。
这些函数支持运行 shell 脚本、命令和可执行文件，并提供超时控制和日志记录功能。

主要功能包括：

- 运行指定的 shell 脚本，并可选择是否静默输出日志
- 支持指定超时时间来限制命令执行的时长
- 在执行命令或脚本时记录开始和结束的日志信息
- 包含执行命令、执行可执行文件的函数，支持参数传递
- 提供原始执行的功能，不记录错误信息
*/

func ShellRun(script string, silent bool, param ...string) (string, error) {

	script = filepath.Clean(script)
	script = filepath.ToSlash(script)

	if !silent {
		loggo.Info("shell Run start %v %v ", script, fmt.Sprint(param))
	}

	var tmpparam []string
	tmpparam = append(tmpparam, script)
	tmpparam = append(tmpparam, param...)

	begin := time.Now()
	cmd := exec.Command("sh", tmpparam...)
	out, err := cmd.CombinedOutput()
	outstr := string(out)
	if err != nil {
		loggo.Warn("shell Run fail %v %v", cmd.Args, outstr)
		return "", err
	}

	if !silent {
		loggo.Info("shell Run ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}

	return outstr, nil
}

func ShellRunTimeout(script string, silent bool, timeout int, param ...string) (string, error) {

	script = filepath.Clean(script)
	script = filepath.ToSlash(script)

	d := time.Now().Add(time.Duration(timeout) * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), d)

	defer cancel() // releases resources if slowOperation completes before timeout elapses
	if !silent {
		loggo.Info("shell Run start %v %v %v ", script, timeout, fmt.Sprint(param))
	}

	var tmpparam []string
	tmpparam = append(tmpparam, script)
	tmpparam = append(tmpparam, param...)

	begin := time.Now()
	cmd := exec.CommandContext(ctx, "sh", tmpparam...)
	out, err := cmd.CombinedOutput()
	outstr := string(out)
	if err != nil {
		loggo.Warn("shell Run fail %v %v %v", cmd.Args, outstr, ctx.Err())
		return "", err
	}

	if !silent {
		loggo.Info("shell Run ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}
	return outstr, nil
}

func ShellRunCommand(command string, silent bool) (string, error) {

	if !silent {
		loggo.Info("shell RunCommand start %v ", command)
	}

	begin := time.Now()
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.CombinedOutput()
	outstr := string(out)
	if err != nil {
		loggo.Warn("shell RunCommand fail %v %v %v", cmd.Args, outstr, err)
		return "", err
	}

	if !silent {
		loggo.Info("shell RunCommand ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}

	return outstr, nil
}

func ShellRunExe(exe string, silent bool, param ...string) (string, error) {

	exe = filepath.Clean(exe)
	exe = filepath.ToSlash(exe)

	if !silent {
		loggo.Info("shell Run start %v %v ", exe, fmt.Sprint(param))
	}

	begin := time.Now()
	cmd := exec.Command(exe, param...)
	out, err := cmd.CombinedOutput()
	outstr := string(out)
	if err != nil {
		loggo.Warn("shell Run fail %v %v %v", cmd.Args, outstr, err)
		return "", err
	}

	if !silent {
		loggo.Info("shell Run ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}

	return outstr, nil
}

func ShellRunExeTimeout(exe string, silent bool, timeout int, param ...string) (string, error) {

	d := time.Now().Add(time.Duration(timeout) * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), d)

	defer cancel() // releases resources if slowOperation completes before timeout elapses

	exe = filepath.Clean(exe)
	exe = filepath.ToSlash(exe)

	if !silent {
		loggo.Info("shell Run start %v %v ", exe, fmt.Sprint(param))
	}

	begin := time.Now()
	cmd := exec.CommandContext(ctx, exe, param...)
	out, err := cmd.CombinedOutput()
	outstr := string(out)
	if err != nil {
		loggo.Warn("shell Run fail %v %v %v", cmd.Args, outstr, err)
		return "", err
	}

	if !silent {
		loggo.Info("shell Run ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}

	return outstr, nil
}

func ShellRunExeRaw(exe string, silent bool, param ...string) string {

	exe = filepath.Clean(exe)
	exe = filepath.ToSlash(exe)

	if !silent {
		loggo.Info("shell Run start %v %v ", exe, fmt.Sprint(param))
	}

	begin := time.Now()
	cmd := exec.Command(exe, param...)
	out, _ := cmd.CombinedOutput()
	outstr := string(out)

	if !silent {
		loggo.Info("shell Run ok %v %v", cmd.Args, time.Now().Sub(begin))
		loggo.Info("%v", outstr)
	}

	return outstr
}
