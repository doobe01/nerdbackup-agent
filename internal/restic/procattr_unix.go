//go:build !windows

package restic

import (
	"os/exec"
	"syscall"
)

// setProcAttr configures the command to start in its own process group.
// This allows SIGSTOP/SIGCONT to target the entire group (restic + children).
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
