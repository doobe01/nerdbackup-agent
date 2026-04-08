//go:build linux

package heartbeat

import "golang.org/x/sys/unix"

func GetTotalMemory() int64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	return int64(info.Totalram) * int64(info.Unit)
}

func GetFreeDisk() int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs("/", &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}
