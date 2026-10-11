//go:build darwin || linux

package platform

import (
	"os/exec"
	"syscall"
)

// setProcessGroupKill 让子进程在独立进程组启动（pgid 为子进程自身 pid），
// 并在 context 超时/取消时向整个进程组发送 SIGKILL。
//
// 默认的 CommandContext 只杀直接子进程：通过 sh 脚本拉起的孙进程
// （例如脚本中后台执行的 sleep/网络命令）会被 init 收养并继续运行，
// 使超时失去资源回收意义。独立进程组 + 组杀可连脚本派生的后代一起清理。
func setProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// cmd.Process 在 Cancel 被调用时必然非空（os/exec 保证）
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
