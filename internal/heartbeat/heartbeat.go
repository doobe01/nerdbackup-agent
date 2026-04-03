package heartbeat

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"golang.org/x/sys/unix"
)

// Start sends a heartbeat every interval until ctx is cancelled.
func Start(ctx context.Context, client *api.Client, agentVersion, resticVersion string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send one immediately
	send(client, agentVersion, resticVersion)

	for {
		select {
		case <-ctx.Done():
			logging.Log.Info().Msg("Heartbeat stopped")
			return
		case <-ticker.C:
			send(client, agentVersion, resticVersion)
		}
	}
}

func send(client *api.Client, agentVersion, resticVersion string) {
	hostname, _ := os.Hostname()

	req := api.HeartbeatRequest{
		AgentVersion:  agentVersion,
		ResticVersion: resticVersion,
		Platform:      runtime.GOOS,
		Arch:          runtime.GOARCH,
		Hostname:      hostname,
		CPUCount:      runtime.NumCPU(),
		MemTotalBytes: getTotalMemory(),
		DiskFreeBytes: getFreeDisk("/"),
	}

	if err := client.SendHeartbeat(req); err != nil {
		logging.Log.Warn().Err(err).Msg("Heartbeat failed")
	} else {
		logging.Log.Debug().Msg("Heartbeat sent")
	}
}

func getTotalMemory() int64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	return int64(info.Totalram) * int64(info.Unit)
}

func getFreeDisk(path string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}
