package heartbeat

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// Start sends a heartbeat every interval until ctx is cancelled.
func Start(ctx context.Context, client *api.Client, agentVersion, resticVersion string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

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
		DiskFreeBytes: getFreeDisk(),
	}

	if err := client.SendHeartbeat(req); err != nil {
		logging.Log.Warn().Err(err).Msg("Heartbeat failed")
	} else {
		logging.Log.Debug().Msg("Heartbeat sent")
	}
}
