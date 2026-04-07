//go:build windows

package process

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	thSuspendResume = 0x0002
	th32csSnapthread = 0x00000004
)

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procSuspendThread = kernel32.NewProc("SuspendThread")
	procResumeThread  = kernel32.NewProc("ResumeThread")
)

// SuspendProcess suspends all threads in a process (Windows equivalent of SIGSTOP).
func SuspendProcess(pid int) error {
	return forEachThread(uint32(pid), func(threadID uint32) error {
		handle, err := windows.OpenThread(thSuspendResume, false, threadID)
		if err != nil {
			return fmt.Errorf("OpenThread(%d): %w", threadID, err)
		}
		defer windows.CloseHandle(handle)

		ret, _, callErr := procSuspendThread.Call(uintptr(handle))
		if ret == 0xFFFFFFFF {
			return fmt.Errorf("SuspendThread(%d): %w", threadID, callErr)
		}
		return nil
	})
}

// ResumeProcess resumes all threads in a process (Windows equivalent of SIGCONT).
func ResumeProcess(pid int) error {
	return forEachThread(uint32(pid), func(threadID uint32) error {
		handle, err := windows.OpenThread(thSuspendResume, false, threadID)
		if err != nil {
			return fmt.Errorf("OpenThread(%d): %w", threadID, err)
		}
		defer windows.CloseHandle(handle)

		ret, _, callErr := procResumeThread.Call(uintptr(handle))
		if ret == 0xFFFFFFFF {
			return fmt.Errorf("ResumeThread(%d): %w", threadID, callErr)
		}
		return nil
	})
}

// forEachThread enumerates all threads of a process and calls fn for each.
func forEachThread(pid uint32, fn func(threadID uint32) error) error {
	snap, err := windows.CreateToolhelp32Snapshot(th32csSnapthread, 0)
	if err != nil {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)

	var entry threadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	// Thread32First
	ret, _, callErr := kernel32.NewProc("Thread32First").Call(
		uintptr(snap),
		uintptr(unsafe.Pointer(&entry)),
	)
	if ret == 0 {
		return fmt.Errorf("Thread32First: %w", callErr)
	}

	for {
		if entry.OwnerProcessID == pid {
			if err := fn(entry.ThreadID); err != nil {
				return err
			}
		}

		entry.Size = uint32(unsafe.Sizeof(entry))
		ret, _, callErr = kernel32.NewProc("Thread32Next").Call(
			uintptr(snap),
			uintptr(unsafe.Pointer(&entry)),
		)
		if ret == 0 {
			break // no more threads
		}
	}

	return nil
}
