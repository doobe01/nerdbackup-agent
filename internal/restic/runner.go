package restic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// Runner wraps the restic CLI.
type Runner struct {
	Binary string
	Env    []string // RESTIC_REPOSITORY, RESTIC_PASSWORD, AWS_ACCESS_KEY_ID, etc.
}

// NewRunner creates a runner with the given environment.
func NewRunner(binary string, repoURI string, password string, storageEnv map[string]string) *Runner {
	env := []string{
		"RESTIC_REPOSITORY=" + repoURI,
		"RESTIC_PASSWORD=" + password,
	}
	for k, v := range storageEnv {
		env = append(env, k+"="+v)
	}
	return &Runner{Binary: binary, Env: env}
}

// Init initializes a new restic repository.
func (r *Runner) Init(ctx context.Context) error {
	_, err := r.run(ctx, "init")
	return err
}

// Backup runs a backup and streams progress.
func (r *Runner) Backup(ctx context.Context, paths []string, excludes []string, tags []string, onProgress func(ProgressEntry)) (*BackupSummary, error) {
	args := []string{"backup", "--json", "--verbose"}
	args = append(args, paths...)
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}

	cmd := r.command(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start restic: %w", err)
	}

	var summary *BackupSummary
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		msgType := extractMessageType(line)

		switch msgType {
		case "status":
			var progress ProgressEntry
			if err := json.Unmarshal(line, &progress); err == nil && onProgress != nil {
				onProgress(progress)
			}
		case "summary":
			var s BackupSummary
			if err := json.Unmarshal(line, &s); err == nil {
				summary = &s
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return summary, fmt.Errorf("restic backup failed: %w", err)
	}
	return summary, nil
}

// Snapshots lists all snapshots.
func (r *Runner) Snapshots(ctx context.Context) ([]Snapshot, error) {
	out, err := r.run(ctx, "snapshots", "--json")
	if err != nil {
		return nil, err
	}
	var snapshots []Snapshot
	if err := json.Unmarshal(out, &snapshots); err != nil {
		return nil, fmt.Errorf("parse snapshots: %w", err)
	}
	return snapshots, nil
}

// Restore restores a snapshot to a target directory.
func (r *Runner) Restore(ctx context.Context, snapshotID string, target string) error {
	_, err := r.run(ctx, "restore", snapshotID, "--target", target)
	return err
}

// Forget removes old snapshots per retention policy.
func (r *Runner) Forget(ctx context.Context, keepLast, keepDaily, keepWeekly, keepMonthly int) error {
	args := []string{"forget", "--prune"}
	if keepLast > 0 {
		args = append(args, "--keep-last", fmt.Sprintf("%d", keepLast))
	}
	if keepDaily > 0 {
		args = append(args, "--keep-daily", fmt.Sprintf("%d", keepDaily))
	}
	if keepWeekly > 0 {
		args = append(args, "--keep-weekly", fmt.Sprintf("%d", keepWeekly))
	}
	if keepMonthly > 0 {
		args = append(args, "--keep-monthly", fmt.Sprintf("%d", keepMonthly))
	}
	_, err := r.run(ctx, args...)
	return err
}

// Check verifies the repository integrity.
func (r *Runner) Check(ctx context.Context) error {
	_, err := r.run(ctx, "check")
	return err
}

// Stats returns repository statistics.
func (r *Runner) Stats(ctx context.Context) (*RepoStats, error) {
	out, err := r.run(ctx, "stats", "--json")
	if err != nil {
		return nil, err
	}
	var stats RepoStats
	if err := json.Unmarshal(out, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (r *Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	return cmd
}

func (r *Runner) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := r.command(ctx, args...)
	logging.Log.Debug().Strs("args", args).Msg("Running restic")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("restic %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

func extractMessageType(line []byte) string {
	// Quick extraction without full JSON parse
	var partial struct {
		MessageType string `json:"message_type"`
	}
	json.Unmarshal(line, &partial)
	return partial.MessageType
}
