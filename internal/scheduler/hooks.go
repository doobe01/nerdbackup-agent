package scheduler

// Trust model for hooks:
// Pre/post backup hooks execute arbitrary shell commands via "sh -c".
// These hooks are configured by the authenticated user who owns the agent,
// delivered via the NerdBackup API config sync (HTTPS + agent token auth).
// They are trusted the same way SSH access or crontab entries are trusted —
// the user has full control over what runs on their own machine.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// runPreHook runs the pre-backup command. Returns error if it fails.
func runPreHook(ctx context.Context, command string) error {
	if command == "" {
		return nil
	}

	logging.Log.Info().Str("command", command).Msg("Running pre-backup hook (shell execution)")

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pre-backup hook failed: %w", err)
	}

	logging.Log.Info().Msg("Pre-backup hook completed")
	return nil
}

// runPostHook runs the post-backup command with result env vars.
func runPostHook(ctx context.Context, command string, status string, snapshotID string, dataAdded int64, filesNew int, duration time.Duration) {
	if command == "" {
		return
	}

	logging.Log.Info().Str("command", command).Msg("Running post-backup hook (shell execution)")

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"NERDBACKUP_STATUS="+status,
		"NERDBACKUP_SNAPSHOT_ID="+snapshotID,
		"NERDBACKUP_BYTES_ADDED="+strconv.FormatInt(dataAdded, 10),
		"NERDBACKUP_FILES_NEW="+strconv.Itoa(filesNew),
		"NERDBACKUP_DURATION="+duration.String(),
	)

	if err := cmd.Run(); err != nil {
		logging.Log.Warn().Err(err).Msg("Post-backup hook failed")
	} else {
		logging.Log.Info().Msg("Post-backup hook completed")
	}
}
