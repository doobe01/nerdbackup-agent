//go:build !windows

package process

import "syscall"

// SuspendProcess pauses a process and all its children (process group).
func SuspendProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGSTOP)
}

// ResumeProcess resumes a paused process and all its children.
func ResumeProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGCONT)
}
