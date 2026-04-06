package pitr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

const (
	defaultWALDir     = "/var/lib/nerdbackup/wal-archive"
	defaultBaseDir    = "/var/lib/nerdbackup/pg-basebackup"
)

// SetupPostgresWAL generates the postgresql.conf changes needed for WAL archiving.
// Returns the config lines to add and the archive directory path.
func SetupPostgresWAL(cfg PITRConfig) (configLines string, archiveDir string, err error) {
	archiveDir = cfg.WALArchiveDir
	if archiveDir == "" {
		archiveDir = defaultWALDir
	}

	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		return "", "", fmt.Errorf("create WAL archive dir: %w", err)
	}

	// Set ownership to postgres user (best-effort)
	_ = exec.Command("chown", "postgres:postgres", archiveDir).Run()

	interval := cfg.WALArchiveInterval
	if interval == 0 {
		interval = 60
	}

	configLines = fmt.Sprintf(`# NerdBackup PITR Configuration
wal_level = replica
archive_mode = on
archive_command = 'cp %%p %s/%%f'
archive_timeout = %d
`, archiveDir, interval)

	logging.Log.Info().
		Str("archive_dir", archiveDir).
		Int("archive_timeout", interval).
		Msg("PostgreSQL WAL archiving configuration generated")

	return configLines, archiveDir, nil
}

// RunBaseBackup executes pg_basebackup and stores the result.
func RunBaseBackup(ctx context.Context, cfg PITRConfig) (string, error) {
	baseDir := cfg.BaseBackupDir
	if baseDir == "" {
		baseDir = defaultBaseDir
	}

	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(baseDir, fmt.Sprintf("base-%s", timestamp))

	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return "", fmt.Errorf("create base backup dir: %w", err)
	}

	connStr := fmt.Sprintf("host=%s port=%d user=%s dbname=%s",
		cfg.ConnectionHost, cfg.ConnectionPort, cfg.User, cfg.DatabaseName)

	args := []string{
		"-D", backupDir,
		"-d", connStr,
		"--format=tar",
		"--gzip",
		"--checkpoint=fast",
		"--progress",
	}

	logging.Log.Info().Str("target", backupDir).Msg("Starting pg_basebackup")

	cmd := exec.CommandContext(ctx, "pg_basebackup", args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", cfg.Password))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pg_basebackup failed: %w", err)
	}

	logging.Log.Info().Str("backup", backupDir).Msg("Base backup completed")
	return backupDir, nil
}

// GetWALStatus returns the current state of WAL archiving.
func GetWALStatus(cfg PITRConfig) WALStatus {
	archiveDir := cfg.WALArchiveDir
	if archiveDir == "" {
		archiveDir = defaultWALDir
	}

	var status WALStatus

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return status
	}

	status.WALArchiveCount = len(entries)

	var latestTime time.Time
	var totalSize int64

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
		}
	}

	status.ArchiveDirSizeBytes = totalSize

	if !latestTime.IsZero() {
		status.LastWALArchived = latestTime.Format(time.RFC3339)
		status.CurrentRPOSeconds = int(time.Since(latestTime).Seconds())
	}

	return status
}

// CleanupOldWALFiles removes WAL files older than the specified duration.
func CleanupOldWALFiles(archiveDir string, maxAge time.Duration) (int, error) {
	if archiveDir == "" {
		archiveDir = defaultWALDir
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(archiveDir, entry.Name()))
			removed++
		}
	}

	return removed, nil
}
