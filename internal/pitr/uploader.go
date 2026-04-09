package pitr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
)

// WALUploader watches the WAL archive directory and uploads new files to S3
// using restic. It runs on a configurable interval and tracks which files
// have already been uploaded to avoid duplicates.
type WALUploader struct {
	config       PITRConfig
	runner       *restic.Runner
	interval     time.Duration
	uploaded     map[string]bool // filename -> uploaded
	mu           sync.Mutex
	lastUploadAt time.Time
}

// NewWALUploader creates a WAL uploader that uses the given restic runner
// to back up WAL files. The runner should be configured with a dedicated
// restic repo (or tagged separately) for WAL archives.
func NewWALUploader(config PITRConfig, runner *restic.Runner, interval time.Duration) *WALUploader {
	return &WALUploader{
		config:   config,
		runner:   runner,
		interval: interval,
		uploaded: make(map[string]bool),
	}
}

// StartWALUploader watches the WAL archive directory and uploads new files
// to S3 via restic. It blocks until ctx is cancelled.
func StartWALUploader(ctx context.Context, config PITRConfig, runner *restic.Runner, interval time.Duration) {
	uploader := NewWALUploader(config, runner, interval)
	uploader.Run(ctx)
}

// Run starts the upload loop. Blocks until ctx is cancelled.
func (u *WALUploader) Run(ctx context.Context) {
	archiveDir := u.config.WALArchiveDir
	if archiveDir == "" {
		archiveDir = defaultWALDir
	}

	logging.Log.Info().
		Str("archive_dir", archiveDir).
		Dur("interval", u.interval).
		Msg("WAL uploader started")

	// Do an initial scan immediately
	u.uploadNewWALFiles(ctx, archiveDir)

	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Log.Info().Msg("WAL uploader stopped")
			return
		case <-ticker.C:
			u.uploadNewWALFiles(ctx, archiveDir)
		}
	}
}

// uploadNewWALFiles scans the archive directory and uploads any WAL files
// that haven't been uploaded yet. Uses restic backup with a "pitr-wal" tag
// so WAL archives are distinguishable from regular backups.
func (u *WALUploader) uploadNewWALFiles(ctx context.Context, archiveDir string) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Log.Warn().Err(err).Str("dir", archiveDir).Msg("Failed to read WAL archive directory")
		}
		return
	}

	// Collect new WAL files (not yet uploaded, not .uploaded marker files)
	var newFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".uploaded") || strings.HasSuffix(name, ".partial") {
			continue
		}

		u.mu.Lock()
		alreadyUploaded := u.uploaded[name]
		u.mu.Unlock()

		if alreadyUploaded {
			continue
		}

		// Also check for .uploaded marker file on disk (survives restarts)
		markerPath := filepath.Join(archiveDir, name+".uploaded")
		if _, statErr := os.Stat(markerPath); statErr == nil {
			u.mu.Lock()
			u.uploaded[name] = true
			u.mu.Unlock()
			continue
		}

		newFiles = append(newFiles, filepath.Join(archiveDir, name))
	}

	if len(newFiles) == 0 {
		return
	}

	logging.Log.Info().Int("count", len(newFiles)).Msg("Uploading new WAL files via restic")

	// Use restic backup to upload the WAL directory with pitr tags.
	// Restic handles dedup, so re-uploading the same directory is efficient.
	summary, err := u.runner.Backup(ctx, restic.BackupOptions{
		Paths: []string{archiveDir},
		Tags: []string{
			"nerdbackup:pitr-wal",
			"nerdbackup:db=" + u.config.DatabaseName,
			"nerdbackup:host=" + u.config.ConnectionHost,
		},
		Excludes: []string{"*.uploaded", "*.partial"},
	}, func(p restic.ProgressEntry) {
		logging.Log.Debug().
			Float64("percent", p.PercentDone*100).
			Msg("WAL upload progress")
	})

	if err != nil {
		logging.Log.Error().Err(err).Msg("WAL upload via restic failed")
		return
	}

	// Mark all files as uploaded
	u.mu.Lock()
	for _, filePath := range newFiles {
		name := filepath.Base(filePath)
		u.uploaded[name] = true

		// Create .uploaded marker file so we survive restarts
		markerPath := filePath + ".uploaded"
		if f, createErr := os.Create(markerPath); createErr == nil {
			f.Close()
		}
	}
	u.lastUploadAt = time.Now()
	u.mu.Unlock()

	logging.Log.Info().
		Int("files_uploaded", len(newFiles)).
		Str("snapshot_id", summary.SnapshotID).
		Int64("data_added", summary.DataAdded).
		Msg("WAL upload completed")
}

// UploadBaseBackup uploads a base backup directory to S3 via restic.
// The backup is tagged with "pitr-base" for easy identification.
func UploadBaseBackup(ctx context.Context, runner *restic.Runner, backupDir string, config PITRConfig) (string, error) {
	logging.Log.Info().Str("backup_dir", backupDir).Msg("Uploading base backup via restic")

	summary, err := runner.Backup(ctx, restic.BackupOptions{
		Paths: []string{backupDir},
		Tags: []string{
			"nerdbackup:pitr-base",
			"nerdbackup:db=" + config.DatabaseName,
			"nerdbackup:host=" + config.ConnectionHost,
			fmt.Sprintf("nerdbackup:base-time=%s", time.Now().Format(time.RFC3339)),
		},
	}, func(p restic.ProgressEntry) {
		logging.Log.Debug().
			Float64("percent", p.PercentDone*100).
			Msg("Base backup upload progress")
	})

	if err != nil {
		return "", fmt.Errorf("base backup upload failed: %w", err)
	}

	logging.Log.Info().
		Str("snapshot_id", summary.SnapshotID).
		Int64("data_added", summary.DataAdded).
		Msg("Base backup upload completed")

	return summary.SnapshotID, nil
}

// LastUploadTime returns the time of the last successful WAL upload.
func (u *WALUploader) LastUploadTime() time.Time {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastUploadAt
}

// UploadedCount returns the number of WAL files that have been uploaded.
func (u *WALUploader) UploadedCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.uploaded)
}
