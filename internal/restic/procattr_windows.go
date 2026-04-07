//go:build windows

package restic

import "os/exec"

// setProcAttr is a no-op on Windows.
// Windows uses thread enumeration for suspend/resume instead of process groups.
func setProcAttr(cmd *exec.Cmd) {}
