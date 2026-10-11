package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/esrrhs/gohome/loggo"
)

// 统一初始化 loggo：禁止落盘（默认配置会在包目录写 default_*.log），
// 这些用例不需要断言日志内容，静默打印即可。
func init() {
	loggo.Ini(loggo.Config{
		Prefix:     "platformharden",
		NoLogFile:  true,
		NoPrint:    true,
		NoLogColor: true,
		Level:      loggo.LEVEL_DEBUG,
	})
}

// writeScript 在临时目录写可执行脚本并返回路径。
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// waitProcessDead 轮询 pid 是否被回收（ESRCH），超时则判定仍存活。
func waitProcessDead(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d still alive after %v", pid, timeout)
}

func TestShellRunTimeoutKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	script := writeScript(t, "spawn.sh", "#!/bin/sh\nsleep 20 &\necho $! > "+pidfile+"\nwait\n")

	start := time.Now()
	out, err := ShellRunTimeout(script, true, 1)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("timeout must return error, out=%q", out)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("timeout not respected, elapsed=%v", elapsed)
	}

	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("grandchild pidfile missing: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Fatalf("bad grandchild pid: %q", string(b))
	}
	// 关键断言：旧实现只杀 sh，sleep 孙进程会成为孤儿存活
	waitProcessDead(t, pid, 2*time.Second)
}

func TestShellRunExeTimeoutKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	// 直接执行带 shebang 的脚本：子进程自身是 group leader，
	// 其后台孙进程必须被组杀覆盖
	script := writeScript(t, "spawnexe.sh", "#!/bin/sh\nsleep 20 &\necho $! > "+pidfile+"\nwait\n")

	start := time.Now()
	_, err := ShellRunExeTimeout(script, true, 1)
	if err == nil {
		t.Fatal("timeout must return error")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("timeout not respected")
	}

	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("grandchild pidfile missing: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("bad grandchild pid: %q", string(b))
	}
	waitProcessDead(t, pid, 2*time.Second)
}

func TestShellRunTimeoutZeroKillsImmediately(t *testing.T) {
	script := writeScript(t, "long.sh", "#!/bin/sh\nsleep 20\n")
	start := time.Now()
	_, err := ShellRunTimeout(script, true, 0)
	if err == nil {
		t.Fatal("already-expired deadline must return error")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("timeout=0 must kill promptly")
	}
}

func TestShellRunTimeoutSuccessWithParams(t *testing.T) {
	script := writeScript(t, "args.sh", "#!/bin/sh\necho \"$#-$1-$2\"\n")
	out, err := ShellRunTimeout(script, true, 5, "aa", "bb")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "2-aa-bb" {
		t.Fatalf("params not passed: %q", out)
	}
}

func TestShellRunFailurePaths(t *testing.T) {
	// 不存在的脚本：sh 退出非零，必须返回 error 与空结果
	missing := filepath.Join(t.TempDir(), "nope.sh")
	if out, err := ShellRun(missing, true); err == nil {
		t.Fatalf("missing script must error, out=%q", out)
	}

	// 脚本显式非零退出：契约上输出被丢弃，返回空串 + error
	script := writeScript(t, "fail.sh", "#!/bin/sh\necho to-stderr-msg >&2\nexit 7\n")
	out, err := ShellRun(script, true)
	if err == nil {
		t.Fatal("exit 7 must produce error")
	}
	if out != "" {
		t.Fatalf("failure must return empty output, got %q", out)
	}
}

func TestShellRunCommandFailureAndSuccess(t *testing.T) {
	// 契约：失败时输出被丢弃，只返回 error 与空串
	out, err := ShellRunCommand("echo via-bash; exit 3", true)
	if err == nil {
		t.Fatal("non-zero exit must error")
	}
	if out != "" {
		t.Fatalf("failure must return empty output, got %q", out)
	}

	out, err = ShellRunCommand("printf 'ok-%s' bash", true)
	if err != nil || out != "ok-bash" {
		t.Fatalf("success path: out=%q err=%v", out, err)
	}
}

func TestShellRunExeSuccessAndFailure(t *testing.T) {
	out, err := ShellRunExe("echo", true, "exe-arg", "second")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exe-arg second") {
		t.Fatalf("exe params missing: %q", out)
	}

	if _, err := ShellRunExe("definitely-not-a-real-exe-xyz123", true); err == nil {
		t.Fatal("missing exe must return error")
	}
}

func TestShellRunExeRawFailureNoErrorReturn(t *testing.T) {
	// Raw 版本只返回字符串，失败时不得 panic（error 被丢弃）
	out := ShellRunExeRaw("definitely-not-a-real-exe-xyz123", true)
	if out != "" {
		t.Logf("raw output on failure: %q", out)
	}
}

func TestShellRunNonSilentSmoke(t *testing.T) {
	// 覆盖 silent=false 的 start/ok 日志分支（loggo 已 NoFile+NoPrint）
	script := writeScript(t, "quiet.sh", "#!/bin/sh\necho silent-off\n")
	if _, err := ShellRun(script, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ShellRunTimeout(script, false, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := ShellRunCommand("echo command-nonsilent", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ShellRunExe("echo", false, "exe-nonsilent"); err != nil {
		t.Fatal(err)
	}
	if _, err := ShellRunExeTimeout("echo", false, 5, "exe-timeout-nonsilent"); err != nil {
		t.Fatal(err)
	}
	ShellRunExeRaw("echo", false, "raw-nonsilent")
	// 超时失败的 non-silent 路径（含 Warn）
	slow := writeScript(t, "slow.sh", "#!/bin/sh\nsleep 20\n")
	if _, err := ShellRunTimeout(slow, false, 1); err == nil {
		t.Fatal("expected timeout error")
	}
	if _, err := ShellRunExeTimeout("sleep", false, 1, "20"); err == nil {
		t.Fatal("expected timeout error")
	}
}
