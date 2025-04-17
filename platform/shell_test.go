package platform

import (
	"github.com/esrrhs/gohome/loggo"
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
