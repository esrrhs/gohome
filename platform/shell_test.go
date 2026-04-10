package platform

import (
	"github.com/esrrhs/gohome/loggo"
	"strings"
	"testing"
)

func TestShell0001(t *testing.T) {
	loggo.Info("start 1")
	ShellRunExeTimeout("sleep", false, 2, "5")
	loggo.Info("end 1")
}

func TestShell0002(t *testing.T) {
	loggo.Info("start 2")
	ShellRunExe("sleep", false, "2")
	loggo.Info("end 2")
}

func TestShellRunCommand(t *testing.T) {
	out, err := ShellRunCommand("echo hello", true)
	t.Logf("ShellRunCommand output: %q, err: %v", out, err)
	if err != nil {
		t.Errorf("ShellRunCommand returned error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("ShellRunCommand output %q does not contain 'hello'", out)
	}
}

func TestShellRunExeRaw(t *testing.T) {
	out := ShellRunExeRaw("echo", true, "world")
	t.Logf("ShellRunExeRaw output: %q", out)
	if !strings.Contains(out, "world") {
		t.Errorf("ShellRunExeRaw output %q does not contain 'world'", out)
	}
}
