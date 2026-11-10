package platform

import "os/exec"

// Windows CommandContext uses its default direct-child Process.Kill cancellation.
// Unix process groups are not available here; descendant-tree cancellation would
// require a Windows Job Object and is not provided by this helper.
func setProcessGroupKill(_ *exec.Cmd) {}
