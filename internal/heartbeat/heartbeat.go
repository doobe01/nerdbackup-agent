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
func Start(ctx context.Context, client *api.Client, agentVersion, resticVersion string, startedAt time.Time, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	send(ctx, client, agentVersion, resticVersion, startedAt)

	for {
		select {
		case <-ctx.Done():
			logging.Log.Info().Msg("Heartbeat stopped")
			return
		case <-ticker.C:
			send(ctx, client, agentVersion, resticVersion, startedAt)
		}
	}
}

// SendOnce sends a single heartbeat via HTTP. Used as a fallback when WebSocket is unavailable.
func SendOnce(ctx context.Context, client *api.Client, agentVersion, resticVersion string, startedAt time.Time) {
	send(ctx, client, agentVersion, resticVersion, startedAt)
}

func send(ctx context.Context, client *api.Client, agentVersion, resticVersion string, startedAt time.Time) {
	hostname, _ := os.Hostname()

	req := api.HeartbeatRequest{
		AgentVersion:  agentVersion,
		ResticVersion: resticVersion,
		Platform:      runtime.GOOS,
		Arch:          runtime.GOARCH,
		Hostname:      hostname,
		UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		CPUCount:      runtime.NumCPU(),
		MemTotalBytes: GetTotalMemory(),
		DiskFreeBytes: GetFreeDisk(),
	}

	resp, err := client.SendHeartbeat(ctx, req)
	if err != nil {
		logging.Log.Warn().Err(err).Msg("Heartbeat failed")
	} else {
		logging.Log.Debug().Bool("config_changed", resp.ConfigChanged).Msg("Heartbeat sent")
	}
}
