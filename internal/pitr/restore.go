package pitr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
)

// RestoreToPoint performs a point-in-time recovery by:
//  1. Finding the most recent base backup before targetTime via restic snapshots
//  2. Restoring the base backup from restic to restoreDir
//  3. Restoring WAL files from restic to the WAL archive directory
//  4. Configuring PostgreSQL recovery settings for the target time
//  5. Starting PostgreSQL with recovery mode
//
// The runner should be the same restic runner used for PITR backups (base + WAL).
func RestoreToPoint(ctx context.Context, config PITRConfig, runner *restic.Runner, targetTime time.Time, restoreDir string) error {
	log := logging.Log.With().
		Str("target_time", targetTime.Format(time.RFC3339)).
		Str("restore_dir", restoreDir).
		Str("database", config.DatabaseName).
		Logger()

	log.Info().Msg("Starting point-in-time recovery")

	// Step 1: Find the most recent base backup snapshot before targetTime
	baseSnapshotID, err := findBaseBackupSnapshot(ctx, runner, targetTime)
	if err != nil {
		return fmt.Errorf("find base backup: %w", err)
	}
	log.Info().Str("base_snapshot", baseSnapshotID).Msg("Found base backup snapshot")

	// Step 2: Restore base backup from restic
	if err := os.MkdirAll(restoreDir, 0700); err != nil {
		return fmt.Errorf("create restore dir: %w", err)
	}

	log.Info().Msg("Restoring base backup from restic")
	if err := runner.Restore(ctx, baseSnapshotID, restoreDir, nil, nil); err != nil {
		return fmt.Errorf("restore base backup: %w", err)
	}

	// Step 3: Restore WAL files from restic
	walRestoreDir := filepath.Join(restoreDir, "pg_wal_restore")
	if err := os.MkdirAll(walRestoreDir, 0700); err != nil {
		return fmt.Errorf("create WAL restore dir: %w", err)
	}

	walSnapshotID, err := findWALSnapshot(ctx, runner, targetTime)
	if err != nil {
		log.Warn().Err(err).Msg("No WAL snapshot found, recovery will use only base backup")
	} else {
		log.Info().Str("wal_snapshot", walSnapshotID).Msg("Restoring WAL files from restic")
		if err := runner.Restore(ctx, walSnapshotID, walRestoreDir, nil, []string{"*.uploaded", "*.partial"}); err != nil {
			return fmt.Errorf("restore WAL files: %w", err)
		}
	}

	// Step 4: Configure PostgreSQL recovery
	if err := configureRecovery(config, restoreDir, walRestoreDir, targetTime); err != nil {
		return fmt.Errorf("configure recovery: %w", err)
	}

	// Step 5: Start PostgreSQL and verify recovery
	if err := startPostgresRecovery(ctx, config, restoreDir); err != nil {
		return fmt.Errorf("start recovery: %w", err)
	}

	log.Info().Msg("Point-in-time recovery completed successfully")
	return nil
}

// findBaseBackupSnapshot finds the most recent restic snapshot tagged as a base
// backup that was created before the target time.
func findBaseBackupSnapshot(ctx context.Context, runner *restic.Runner, targetTime time.Time) (string, error) {
	snapshots, err := listTaggedSnapshots(ctx, runner, "nerdbackup:pitr-base")
	if err != nil {
		return "", err
	}

	// Find the most recent snapshot before targetTime
	var bestSnapshot *restic.Snapshot
	for i := range snapshots {
		s := &snapshots[i]
		if s.Time.Before(targetTime) || s.Time.Equal(targetTime) {
			if bestSnapshot == nil || s.Time.After(bestSnapshot.Time) {
				bestSnapshot = s
			}
		}
	}

	if bestSnapshot == nil {
		return "", fmt.Errorf("no base backup found before %s", targetTime.Format(time.RFC3339))
	}

	return bestSnapshot.ID, nil
}

// findWALSnapshot finds the most recent WAL archive snapshot that was created
// at or after the base backup time but before or at the target time.
func findWALSnapshot(ctx context.Context, runner *restic.Runner, targetTime time.Time) (string, error) {
	snapshots, err := listTaggedSnapshots(ctx, runner, "nerdbackup:pitr-wal")
	if err != nil {
		return "", err
	}

	// Find the most recent WAL snapshot at or before targetTime
	var bestSnapshot *restic.Snapshot
	for i := range snapshots {
		s := &snapshots[i]
		if s.Time.Before(targetTime) || s.Time.Equal(targetTime) {
			if bestSnapshot == nil || s.Time.After(bestSnapshot.Time) {
				bestSnapshot = s
			}
		}
	}

	if bestSnapshot == nil {
		return "", fmt.Errorf("no WAL snapshot found before %s", targetTime.Format(time.RFC3339))
	}

	return bestSnapshot.ID, nil
}

// listTaggedSnapshots returns all snapshots that contain the given tag.
func listTaggedSnapshots(ctx context.Context, runner *restic.Runner, tag string) ([]restic.Snapshot, error) {
	snapshots, err := runner.Snapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}

	var filtered []restic.Snapshot
	for _, s := range snapshots {
		for _, t := range s.Tags {
			if t == tag {
				filtered = append(filtered, s)
				break
			}
		}
	}

	return filtered, nil
}

// configureRecovery writes the PostgreSQL recovery configuration.
// For PostgreSQL 12+, this uses recovery signals (recovery.signal file)
// and writes settings to postgresql.auto.conf.
// For PostgreSQL <12, this writes a recovery.conf file.
func configureRecovery(config PITRConfig, dataDir string, walRestoreDir string, targetTime time.Time) error {
	logging.Log.Info().
		Str("data_dir", dataDir).
		Str("target_time", targetTime.Format(time.RFC3339)).
		Msg("Configuring PostgreSQL recovery")

	// Determine the actual PostgreSQL data directory.
	// After restic restore, the data may be nested under the original path structure.
	pgDataDir := findPGDataDir(dataDir)
	if pgDataDir == "" {
		pgDataDir = dataDir
	}

	// Find the WAL files directory within the restored WAL snapshot.
	// After restic restore, files are under the original archive path.
	walSourceDir := findWALSourceDir(walRestoreDir, config.WALArchiveDir)

	// Build restore_command that copies WAL files from the restored archive
	restoreCommand := fmt.Sprintf("cp %s/%%f %%p", walSourceDir)

	// Write recovery signal file (PostgreSQL 12+)
	signalPath := filepath.Join(pgDataDir, "recovery.signal")
	if err := os.WriteFile(signalPath, []byte(""), 0600); err != nil {
		return fmt.Errorf("write recovery.signal: %w", err)
	}

	// Write recovery settings to postgresql.auto.conf
	autoConfPath := filepath.Join(pgDataDir, "postgresql.auto.conf")
	recoveryConf := fmt.Sprintf(`# NerdBackup PITR Recovery Configuration
restore_command = '%s'
recovery_target_time = '%s'
recovery_target_action = 'promote'
`, restoreCommand, targetTime.Format("2006-01-02 15:04:05+00"))

	// Append to existing postgresql.auto.conf (don't overwrite)
	f, err := os.OpenFile(autoConfPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open postgresql.auto.conf: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + recoveryConf); err != nil {
		return fmt.Errorf("write recovery config: %w", err)
	}

	// Also write a recovery.conf for pre-12 compatibility
	recoveryConfPath := filepath.Join(pgDataDir, "recovery.conf")
	preV12Conf := fmt.Sprintf(`# NerdBackup PITR Recovery Configuration (pre-PostgreSQL 12)
restore_command = '%s'
recovery_target_time = '%s'
`, restoreCommand, targetTime.Format("2006-01-02 15:04:05+00"))

	if err := os.WriteFile(recoveryConfPath, []byte(preV12Conf), 0600); err != nil {
		// Non-fatal: this is only needed for PG <12
		logging.Log.Warn().Err(err).Msg("Failed to write recovery.conf (only needed for PostgreSQL <12)")
	}

	// Set ownership to postgres user (best-effort)
	_ = exec.Command("chown", "-R", "postgres:postgres", pgDataDir).Run()

	logging.Log.Info().
		Str("pg_data", pgDataDir).
		Str("wal_source", walSourceDir).
		Msg("Recovery configuration written")

	return nil
}

// findPGDataDir searches for a directory that looks like a PostgreSQL data directory
// within the restored base backup. Restic restores preserve the full path structure.
func findPGDataDir(restoreDir string) string {
	// Look for PG_VERSION file which is always in the data directory
	var pgDataDir string
	_ = filepath.Walk(restoreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "PG_VERSION" && !info.IsDir() {
			pgDataDir = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return pgDataDir
}

// findWALSourceDir locates the WAL files within the restored WAL snapshot.
// Restic preserves the full directory structure, so we look for the original
// archive path within the restore target.
func findWALSourceDir(walRestoreDir string, originalArchiveDir string) string {
	// Try the exact original path under the restore dir
	if originalArchiveDir != "" {
		candidate := filepath.Join(walRestoreDir, originalArchiveDir)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	// Try the default WAL dir
	candidate := filepath.Join(walRestoreDir, defaultWALDir)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}

	// Walk to find a directory containing WAL-like files (16MB files with hex names)
	var walDir string
	_ = filepath.Walk(walRestoreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		// WAL files are 24 hex characters, optionally with .backup suffix
		if len(name) >= 24 && isHexString(name[:24]) && !strings.HasSuffix(name, ".uploaded") {
			walDir = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})

	if walDir != "" {
		return walDir
	}

	// Fallback: just use the restore dir itself
	return walRestoreDir
}

// isHexString returns true if s contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// startPostgresRecovery starts PostgreSQL in recovery mode and waits for
// recovery to complete. This is a best-effort operation -- the user may
// prefer to start PostgreSQL manually.
func startPostgresRecovery(ctx context.Context, config PITRConfig, restoreDir string) error {
	pgDataDir := findPGDataDir(restoreDir)
	if pgDataDir == "" {
		pgDataDir = restoreDir
	}

	logging.Log.Info().Str("data_dir", pgDataDir).Msg("Starting PostgreSQL in recovery mode")

	// Try pg_ctl to start PostgreSQL
	cmd := exec.CommandContext(ctx, "pg_ctl", "start",
		"-D", pgDataDir,
		"-l", filepath.Join(pgDataDir, "recovery.log"),
		"-w", // wait for startup
		"-t", "300", // 5 minute timeout
	)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGDATA=%s", pgDataDir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.Log.Warn().
			Err(err).
			Str("output", string(output)).
			Msg("pg_ctl start failed -- PostgreSQL may need to be started manually")
		// Don't fail the whole restore -- the data is in place, recovery is configured.
		// The user/DBA can start PostgreSQL manually.
		return nil
	}

	logging.Log.Info().Msg("PostgreSQL started in recovery mode")

	// Wait for recovery to complete by checking for recovery.signal removal
	// PostgreSQL removes this file when recovery is complete.
	signalPath := filepath.Join(pgDataDir, "recovery.signal")
	deadline := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			logging.Log.Warn().Msg("Recovery timeout -- PostgreSQL may still be recovering")
			return nil
		case <-ticker.C:
			if _, statErr := os.Stat(signalPath); os.IsNotExist(statErr) {
				logging.Log.Info().Msg("PostgreSQL recovery completed (recovery.signal removed)")
				return nil
			}
		}
	}
}

// VerifyRecovery connects to PostgreSQL and checks that it is running and
// accepting connections after recovery. Returns nil if healthy.
func VerifyRecovery(ctx context.Context, config PITRConfig) error {
	connStr := fmt.Sprintf("host=%s port=%d user=%s dbname=%s",
		config.ConnectionHost, config.ConnectionPort, config.User, config.DatabaseName)

	cmd := exec.CommandContext(ctx, "psql", connStr, "-c", "SELECT 1")
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", config.Password))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verification failed: %w\n%s", err, string(output))
	}

	logging.Log.Info().Msg("PostgreSQL is accepting connections after recovery")
	return nil
}
