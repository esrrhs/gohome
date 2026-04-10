package platform

import (
	"os"
	"strings"
	"testing"

	"github.com/esrrhs/gohome/loggo"
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

func TestShellRun(t *testing.T) {
// Create a temp shell script
f, err := os.CreateTemp("", "test_shell_*.sh")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.WriteString("#!/bin/sh\necho hello from script\n")
f.Close()
os.Chmod(f.Name(), 0755)

out, err := ShellRun(f.Name(), true)
t.Logf("ShellRun output: %q, err: %v", out, err)
if err != nil {
t.Errorf("ShellRun returned error: %v", err)
}
if !strings.Contains(out, "hello from script") {
t.Errorf("ShellRun output %q does not contain expected text", out)
}
}

func TestShellRunTimeout(t *testing.T) {
// Create a temp shell script
f, err := os.CreateTemp("", "test_shell_timeout_*.sh")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
f.WriteString("#!/bin/sh\necho timeout test\n")
f.Close()
os.Chmod(f.Name(), 0755)

out, err := ShellRunTimeout(f.Name(), true, 5)
t.Logf("ShellRunTimeout output: %q, err: %v", out, err)
if err != nil {
t.Errorf("ShellRunTimeout returned error: %v", err)
}
if !strings.Contains(out, "timeout test") {
t.Errorf("ShellRunTimeout output %q does not contain expected text", out)
}
}
