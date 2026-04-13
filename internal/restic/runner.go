package restic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

// Runner wraps the restic CLI.
type Runner struct {
	Binary string
	Env    []string
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

// IsInitialized checks if the repo is initialized by running snapshots.
func (r *Runner) IsInitialized(ctx context.Context) bool {
	_, err := r.run(ctx, "snapshots", "--json", "--quiet")
	return err == nil
}

// UnlockIfStale removes locks older than maxAge.
func (r *Runner) UnlockIfStale(ctx context.Context, maxAge time.Duration) error {
	out, err := r.run(ctx, "list", "locks", "--json")
	if err != nil {
		// If listing fails, try a blanket unlock
		logging.Log.Debug().Msg("Could not list locks, attempting unlock")
		_, unlockErr := r.run(ctx, "unlock")
		return unlockErr
	}

	// restic list locks --json outputs one hash per line, not a JSON array.
	// If output is empty or just hashes, there are no structured locks to check.
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return nil // No locks
	}

	var locks []Lock
	if err := json.Unmarshal(out, &locks); err != nil {
		// Output is lock IDs (hashes), not JSON lock objects — just return.
		// Stale locks were already cleared by the caller via UnlockIfStale(0).
		return nil
	}

	cutoff := time.Now().Add(-maxAge)
	for _, lock := range locks {
		if lock.Time.Before(cutoff) {
			logging.Log.Warn().
				Str("hostname", lock.Hostname).
				Time("locked_at", lock.Time).
				Msg("Removing stale lock")
			if _, err := r.run(ctx, "unlock"); err != nil {
				return fmt.Errorf("failed to unlock stale lock: %w", err)
			}
			break // unlock removes all locks
		}
	}

	return nil
}

// Backup runs a backup and streams progress.
func (r *Runner) Backup(ctx context.Context, opts BackupOptions, onProgress func(ProgressEntry)) (*BackupSummary, error) {
	args := []string{"backup", "--json", "--verbose"}

	// On Windows, use Volume Shadow Copy for consistent access to locked/protected files
	if runtime.GOOS == "windows" {
		args = append(args, "--use-fs-snapshot")
	}

	args = append(args, opts.Paths...)
	for _, e := range opts.Excludes {
		args = append(args, "--exclude", e)
	}
	for _, t := range opts.Tags {
		args = append(args, "--tag", t)
	}
	if opts.BandwidthLimitKiB > 0 {
		args = append(args, "--limit-upload", strconv.Itoa(opts.BandwidthLimitKiB))
		args = append(args, "--limit-download", strconv.Itoa(opts.BandwidthLimitKiB))
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

	// Notify caller of the PID (used for pause/resume)
	if opts.OnStarted != nil && cmd.Process != nil {
		opts.OnStarted(cmd.Process.Pid)
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

// Dump outputs a single file's contents from a snapshot.
func (r *Runner) Dump(ctx context.Context, snapshotID string, filePath string) ([]byte, error) {
	return r.run(ctx, "dump", snapshotID, filePath)
}

// LsFiles lists files in a snapshot, limited to maxFiles entries.
func (r *Runner) LsFiles(ctx context.Context, snapshotID string, maxFiles int) ([]FileEntry, error) {
	out, err := r.run(ctx, "ls", "--json", snapshotID)
	if err != nil {
		return nil, err
	}

	var files []FileEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Type       string `json:"type"`
			Path       string `json:"path"`
			Size       int64  `json:"size"`
			ModifyTime string `json:"mtime"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "file" {
			files = append(files, FileEntry{
				Path:       entry.Path,
				Size:       entry.Size,
				ModifiedAt: entry.ModifyTime,
			})
			if len(files) >= maxFiles {
				break
			}
		}
	}
	return files, nil
}

// Restore restores a snapshot to a target directory.
func (r *Runner) Restore(ctx context.Context, snapshotID string, target string, includes []string, excludes []string) error {
	args := []string{"restore", snapshotID, "--target", target}
	for _, p := range includes {
		args = append(args, "--include", p)
	}
	for _, p := range excludes {
		args = append(args, "--exclude", p)
	}
	_, err := r.run(ctx, args...)
	return err
}

// Forget removes old snapshots per retention policy.
func (r *Runner) Forget(ctx context.Context, keepLast, keepDaily, keepWeekly, keepMonthly int) error {
	args := []string{"forget", "--prune"}
	if keepLast > 0 {
		args = append(args, "--keep-last", strconv.Itoa(keepLast))
	}
	if keepDaily > 0 {
		args = append(args, "--keep-daily", strconv.Itoa(keepDaily))
	}
	if keepWeekly > 0 {
		args = append(args, "--keep-weekly", strconv.Itoa(keepWeekly))
	}
	if keepMonthly > 0 {
		args = append(args, "--keep-monthly", strconv.Itoa(keepMonthly))
	}
	_, err := r.run(ctx, args...)
	return err
}

// ForgetSnapshot removes a specific snapshot by ID without pruning.
func (r *Runner) ForgetSnapshot(ctx context.Context, snapshotID string) error {
	_, err := r.run(ctx, "forget", snapshotID)
	return err
}

// Prune removes unreferenced data from the repository.
func (r *Runner) Prune(ctx context.Context) error {
	_, err := r.run(ctx, "prune")
	return err
}

// Check verifies the repository integrity.
func (r *Runner) Check(ctx context.Context) error {
	_, err := r.run(ctx, "check")
	return err
}

// CheckVerbose verifies repository integrity and returns the output.
func (r *Runner) CheckVerbose(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "check")
	return string(out), err
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

// SizeStats runs restic stats in both modes to get true dedup/compression ratio.
// raw-data = actual bytes stored on disk, restore-size = logical size of all data.
func (r *Runner) SizeStats(ctx context.Context) (*RepoSizeStats, error) {
	rawOut, err := r.run(ctx, "stats", "--mode", "raw-data", "--json")
	if err != nil {
		return nil, fmt.Errorf("raw-data stats: %w", err)
	}
	var rawStats RepoStats
	if err := json.Unmarshal(rawOut, &rawStats); err != nil {
		return nil, fmt.Errorf("parse raw-data: %w", err)
	}

	restoreOut, err := r.run(ctx, "stats", "--mode", "restore-size", "--json")
	if err != nil {
		return nil, fmt.Errorf("restore-size stats: %w", err)
	}
	var restoreStats RepoStats
	if err := json.Unmarshal(restoreOut, &restoreStats); err != nil {
		return nil, fmt.Errorf("parse restore-size: %w", err)
	}

	return &RepoSizeStats{
		RawSize:     rawStats.TotalSize,
		RestoreSize: restoreStats.TotalSize,
	}, nil
}

// Version returns the restic version string.
func (r *Runner) Version(ctx context.Context) string {
	out, err := r.run(ctx, "version")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func (r *Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	setProcAttr(cmd) // set process group on Unix for pause/resume
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
	var partial struct {
		MessageType string `json:"message_type"`
	}
	_ = json.Unmarshal(line, &partial)
	return partial.MessageType
}
