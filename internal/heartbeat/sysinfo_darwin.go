//go:build darwin

package heartbeat

import (
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func GetTotalMemory() int64 {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	val, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return val
}

func GetFreeDisk() int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs("/", &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}
