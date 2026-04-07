package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
	"github.com/doobe01/nerdbackup-agent/internal/ws"
	"github.com/robfig/cron/v3"
)

// Scheduler syncs config from the API and manages cron-scheduled backups.
type Scheduler struct {
	client       *api.Client
	resticBinary string
	agentID      string
	hostname     string
	cron         *cron.Cron
	lastRepos    []api.RepoConfig
	syncInterval time.Duration
	cfg          *config.AgentConfig
	backupCounts map[string]int // repo ID → backup count since last health check
	wsClient     *ws.Client    // optional WebSocket client for real-time progress
}

// New creates a scheduler.
func New(client *api.Client, resticBinary, agentID string, cfg *config.AgentConfig, syncInterval time.Duration) *Scheduler {
	hostname, _ := os.Hostname()
	return &Scheduler{
		client:       client,
		resticBinary: resticBinary,
		agentID:      agentID,
		hostname:     hostname,
		cron:         cron.New(),
		syncInterval: syncInterval,
		cfg:          cfg,
		backupCounts: make(map[string]int),
	}
}

// SetWSClient sets the WebSocket client used for real-time progress streaming.
func (s *Scheduler) SetWSClient(wsClient *ws.Client) {
	s.wsClient = wsClient
}

// HandleCommand processes a command received from the WebSocket server.
// Commands are dispatched from the server (dashboard/API) to the agent in real time.
func (s *Scheduler) HandleCommand(cmd ws.Command) {
	log := logging.Log.With().Str("action", cmd.Action).Str("job_id", cmd.JobID).Logger()
	log.Info().Msg("Received command via WebSocket")

	switch cmd.Action {
	case "start_backup":
		// Extract repo ID from command data
		var data struct {
			RepoID string `json:"repo_id"`
			JobID  string `json:"job_id"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}
		if data.RepoID == "" && cmd.JobID != "" {
			data.JobID = cmd.JobID
		}

		// Find the repo and run backup in background
		go func() {
			repos := s.lastRepos
			if len(repos) == 0 {
				log.Warn().Msg("No repos configured, cannot start backup")
				return
			}

			var targetRepo *api.RepoConfig
			for i := range repos {
				if data.RepoID != "" && repos[i].ID == data.RepoID {
					targetRepo = &repos[i]
					break
				}
			}
			if targetRepo == nil && len(repos) > 0 {
				targetRepo = &repos[0]
			}
			if targetRepo == nil {
				log.Warn().Msg("No matching repo for backup command")
				return
			}

			ctx := context.Background()
			// Auto-init repo if needed (first backup after policy creation)
			s.autoInitRepo(ctx, *targetRepo)
			s.runBackup(ctx, *targetRepo, data.JobID)
		}()

	case "cancel":
		// Cancel is handled via context cancellation in the restic runner.
		// For now, log it. Full cancellation requires tracking running processes
		// per job, which is a follow-up task.
		log.Info().Msg("Cancel command received (process cancellation is a follow-up task)")

	case "pause":
		log.Info().Msg("Pause command received (process signaling is a follow-up task)")

	case "resume":
		log.Info().Msg("Resume command received (process signaling is a follow-up task)")

	case "config_update":
		// Force an immediate config re-sync
		log.Info().Msg("Config update command received, triggering re-sync")
		go func() {
			ctx := context.Background()
			s.syncAndSchedule(ctx)
		}()

	default:
		log.Warn().Msg("Unknown command action")
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) {
	// Flush any pending reports from previous run
	s.client.FlushPendingReports(ctx)

	s.syncAndSchedule(ctx)
	s.cron.Start()

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Log.Info().Msg("Scheduler stopped")
			s.cron.Stop()
			return
		case <-ticker.C:
			s.syncAndSchedule(ctx)
		}
	}
}

func (s *Scheduler) syncAndSchedule(ctx context.Context) {
	repos, changed, err := s.client.GetRepos(ctx)
	if err != nil {
		logging.Log.Warn().Err(err).Msg("Failed to sync repo config")
		return
	}

	if changed {
		logging.Log.Info().Int("repos", len(repos)).Msg("Config synced, rebuilding schedules")

		// Auto-initialize new repos
		for _, repo := range repos {
			s.autoInitRepo(ctx, repo)
		}

		// Rebuild cron entries
		for _, entry := range s.cron.Entries() {
			s.cron.Remove(entry.ID)
		}

		for _, repo := range repos {
			if repo.ScheduleCron == "" {
				continue
			}
			r := repo // capture
			_, err := s.cron.AddFunc(r.ScheduleCron, func() {
				s.runBackup(ctx, r)
			})
			if err != nil {
				logging.Log.Error().Err(err).Str("repo", r.ID).Str("cron", r.ScheduleCron).Msg("Invalid cron expression")
			}
		}

		s.lastRepos = repos
	}

	// Always check for pending actions from the dashboard (regardless of config changes)
	activeRepos := s.lastRepos
	if len(activeRepos) == 0 {
		activeRepos = repos
	}
	s.checkPendingRestores(ctx, activeRepos)
	s.checkPendingBackups(ctx, activeRepos)
	s.checkPendingFileDumps(ctx, activeRepos)
}

func (s *Scheduler) autoInitRepo(ctx context.Context, repo api.RepoConfig) {
	if s.cfg.IsRepoInitialized(repo.ID) {
		return
	}

	storageEnv := buildStorageEnv(repo.StorageConfig)
	runner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

	if runner.IsInitialized(ctx) {
		s.cfg.MarkRepoInitialized(repo.ID)
		_ = config.Save(s.cfg)
		return
	}

	logging.Log.Info().Str("repo", repo.ID).Msg("Initializing new restic repo")
	if err := runner.Init(ctx); err != nil {
		logging.Log.Error().Err(err).Str("repo", repo.ID).Msg("Failed to init repo")
		return
	}

	s.cfg.MarkRepoInitialized(repo.ID)
	_ = config.Save(s.cfg)
	logging.Log.Info().Str("repo", repo.ID).Msg("Repo initialized")
}

func (s *Scheduler) runBackup(ctx context.Context, repo api.RepoConfig, dashboardJobID ...string) {
	log := logging.Log.With().Str("repo", repo.ID).Logger()
	log.Info().Strs("paths", repo.Paths).Msg("Starting scheduled backup")

	// Run pre-backup hook
	if err := runPreHook(ctx, repo.PreBackupCommand); err != nil {
		log.Error().Err(err).Msg("Pre-backup hook failed — skipping backup")
		return
	}

	startedAt := time.Now()
	storageEnv := buildStorageEnv(repo.StorageConfig)
	runner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

	// Remove stale locks before backup
	if err := runner.UnlockIfStale(ctx, 30*time.Minute); err != nil {
		log.Warn().Err(err).Msg("Stale lock removal failed")
	}

	// Merge preset + custom exclude patterns
	excludes := restic.MergeExcludes(repo.ExcludePresets, repo.ExcludePatterns)

	// Build tags with NerdBackup metadata
	tags := append(repo.Tags,
		"nerdbackup:agent_id="+s.agentID,
		"nerdbackup:repo_id="+repo.ID,
		"nerdbackup:hostname="+s.hostname,
		"nerdbackup:trigger=scheduled",
	)
	if repo.PolicyID != "" {
		tags = append(tags, "nerdbackup:policy_id="+repo.PolicyID)
	}

	// Run backup with progress reporting
	lastProgressReport := time.Time{}
	summary, err := runner.Backup(ctx, restic.BackupOptions{
		Paths:             repo.Paths,
		Excludes:          excludes,
		Tags:              tags,
		BandwidthLimitKiB: repo.BandwidthLimitKiB,
	}, func(p restic.ProgressEntry) {
		log.Debug().Float64("percent", p.PercentDone*100).Int64("bytes", p.BytesDone).Msg("Progress")

		// Report progress — prefer WebSocket (instant), fall back to HTTP (best-effort)
		if time.Since(lastProgressReport) > 5*time.Second {
			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.SendProgress(ws.ProgressData{
					RepoID:         repo.ID,
					PercentDone:    p.PercentDone,
					BytesProcessed: p.BytesDone,
					FilesProcessed: p.FilesDone,
					StartedAt:      startedAt.Format(time.RFC3339),
				})
			} else {
				s.client.ReportProgress(ctx, api.ProgressReport{
					RepoID:         repo.ID,
					PercentDone:    p.PercentDone,
					BytesProcessed: p.BytesDone,
					FilesProcessed: p.FilesDone,
					StartedAt:      startedAt.Format(time.RFC3339),
				})
			}
			lastProgressReport = time.Now()
		}
	})

	completedAt := time.Now()

	// Build job report
	djID := ""
	if len(dashboardJobID) > 0 {
		djID = dashboardJobID[0]
	}

	report := api.JobReportRequest{
		RepoID:         repo.ID,
		PolicyID:       repo.PolicyID,
		DashboardJobID: djID,
		Operation:      "backup",
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
	}

	if err != nil {
		report.Status = "failed"
		report.ErrorMessage = err.Error()
		log.Error().Err(err).Msg("Backup failed")
	} else {
		report.Status = "completed"
		report.ResticSnapshotID = summary.SnapshotID
		report.Stats = api.JobStats{
			FilesNew:            summary.FilesNew,
			FilesChanged:        summary.FilesChanged,
			FilesUnmodified:     summary.FilesUnmodified,
			DirsNew:             summary.DirsNew,
			DataAddedBytes:      summary.DataAdded,
			TotalFilesProcessed: summary.TotalFilesProcessed,
			TotalBytesProcessed: summary.TotalBytesProcessed,
			TotalDurationSec:    int(summary.TotalDuration),
		}
		log.Info().Str("snapshot", summary.SnapshotID).Int64("added", summary.DataAdded).Msg("Backup completed")

		// Capture file listing for dashboard browsing (max 500 files)
		if files, lsErr := runner.LsFiles(ctx, summary.SnapshotID, 500); lsErr == nil && len(files) > 0 {
			fileList := make([]map[string]interface{}, len(files))
			for i, f := range files {
				fileList[i] = map[string]interface{}{"path": f.Path, "size": f.Size, "modified_at": f.ModifiedAt}
			}
			report.Files = fileList
		}

		// Update last backup time
		s.cfg.LastBackupAt = completedAt.Format(time.RFC3339)
		_ = config.Save(s.cfg)
	}

	// Report to API — prefer WebSocket (instant), fall back to HTTP (with retry + pending persistence)
	if s.wsClient != nil && s.wsClient.IsConnected() {
		if wsErr := s.wsClient.SendJobReport(report); wsErr != nil {
			log.Warn().Err(wsErr).Msg("WebSocket job report failed, falling back to HTTP")
			_ = s.client.ReportJob(ctx, report)
		}
	} else {
		_ = s.client.ReportJob(ctx, report)
	}

	// Run post-backup hook (runs even if backup failed)
	snapshotID := ""
	dataAdded := int64(0)
	filesNew := 0
	if summary != nil {
		snapshotID = summary.SnapshotID
		dataAdded = summary.DataAdded
		filesNew = summary.FilesNew
	}
	runPostHook(ctx, repo.PostBackupCommand, report.Status, snapshotID, dataAdded, filesNew, completedAt.Sub(startedAt))

	// Health check every N backups
	s.backupCounts[repo.ID]++
	if repo.CheckEveryNBackups > 0 && s.backupCounts[repo.ID]%repo.CheckEveryNBackups == 0 {
		log.Info().Msg("Running periodic health check")
		if checkErr := runner.Check(ctx); checkErr != nil {
			log.Error().Err(checkErr).Msg("Health check failed")
		} else {
			log.Info().Msg("Health check passed")
		}
	}
}

func (s *Scheduler) checkPendingRestores(ctx context.Context, repos []api.RepoConfig) {
	restores, err := s.client.GetPendingRestores(ctx)
	if err != nil {
		logging.Log.Debug().Err(err).Msg("Failed to check pending restores")
		return
	}

	if len(restores) == 0 {
		return
	}

	logging.Log.Info().Int("count", len(restores)).Msg("Processing pending restores")

	for _, restore := range restores {
		// Find the repo that has this snapshot
		var targetRepo *api.RepoConfig
		for i := range repos {
			targetRepo = &repos[i]
			break // Use first repo for now — snapshots are per-repo
		}

		if targetRepo == nil {
			logging.Log.Error().Str("snapshot", restore.SnapshotID).Msg("No repo available for restore")
			continue
		}

		// Build restic runner for this repo
		runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))

		logging.Log.Info().
			Str("snapshot", restore.SnapshotID).
			Str("target", restore.TargetPath).
			Strs("include", restore.IncludePaths).
			Msg("Starting restore")

		err := runner.Restore(ctx, restore.SnapshotID, restore.TargetPath, restore.IncludePaths, restore.ExcludePaths)
		if err != nil {
			logging.Log.Error().Err(err).Str("snapshot", restore.SnapshotID).Msg("Restore failed")
			// Report failure
			_ = s.client.ReportJob(ctx, api.JobReportRequest{
				RepoID:    targetRepo.ID,
				Operation: "restore",
				Status:    "failed",
				StartedAt: time.Now(),
				CompletedAt: time.Now(),
				ResticSnapshotID: restore.SnapshotID,
				ErrorMessage: err.Error(),
			})
		} else {
			logging.Log.Info().Str("snapshot", restore.SnapshotID).Str("target", restore.TargetPath).Msg("Restore completed")
			_ = s.client.ReportJob(ctx, api.JobReportRequest{
				RepoID:    targetRepo.ID,
				Operation: "restore",
				Status:    "completed",
				StartedAt: time.Now(),
				CompletedAt: time.Now(),
				ResticSnapshotID: restore.SnapshotID,
			})
		}
	}
}

func (s *Scheduler) checkPendingBackups(ctx context.Context, repos []api.RepoConfig) {
	backups, err := s.client.GetPendingBackups(ctx)
	if err != nil {
		logging.Log.Debug().Err(err).Msg("Failed to check pending backups")
		return
	}

	if len(backups) == 0 {
		return
	}

	logging.Log.Info().Int("count", len(backups)).Msg("Processing pending backup triggers")

	for _, backup := range backups {
		// Find the matching repo
		var targetRepo *api.RepoConfig
		for i := range repos {
			if repos[i].ID == backup.RepoID {
				targetRepo = &repos[i]
				break
			}
		}

		// If no specific repo found, use first repo
		if targetRepo == nil && len(repos) > 0 {
			targetRepo = &repos[0]
		}

		if targetRepo == nil {
			logging.Log.Error().Str("repoId", backup.RepoID).Msg("No repo available for triggered backup")
			continue
		}

		logging.Log.Info().Str("repo", targetRepo.ID).Str("jobId", backup.JobID).Msg("Running dashboard-triggered backup")
		s.runBackup(ctx, *targetRepo, backup.JobID)
	}
}

func (s *Scheduler) checkPendingFileDumps(ctx context.Context, repos []api.RepoConfig) {
	dumps, err := s.client.GetPendingFileDumps(ctx)
	if err != nil {
		logging.Log.Debug().Err(err).Msg("Failed to check pending file dumps")
		return
	}

	if len(dumps) == 0 {
		return
	}

	logging.Log.Info().Int("count", len(dumps)).Msg("Processing pending file dumps")

	for _, dump := range dumps {
		// Use first repo (file dumps don't specify which repo)
		var targetRepo *api.RepoConfig
		if len(repos) > 0 {
			targetRepo = &repos[0]
		}

		if targetRepo == nil {
			logging.Log.Error().Str("requestId", dump.RequestID).Msg("No repo available for file dump")
			continue
		}

		runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))

		logging.Log.Info().
			Str("snapshot", dump.SnapshotID).
			Str("file", dump.FilePath).
			Msg("Dumping file from snapshot")

		data, err := runner.Dump(ctx, dump.SnapshotID, dump.FilePath)
		if err != nil {
			logging.Log.Error().Err(err).Str("file", dump.FilePath).Msg("File dump failed")
			continue
		}

		// Upload result back to server
		fileName := dump.FilePath
		if idx := len(fileName) - 1; idx >= 0 {
			// Use just the filename, not full path
			for i := len(fileName) - 1; i >= 0; i-- {
				if fileName[i] == '/' || fileName[i] == '\\' {
					fileName = fileName[i+1:]
					break
				}
			}
		}

		if err := s.client.UploadFileDump(ctx, dump.RequestID, data, fileName); err != nil {
			logging.Log.Error().Err(err).Str("requestId", dump.RequestID).Msg("Failed to upload file dump")
		} else {
			logging.Log.Info().Str("requestId", dump.RequestID).Int("size", len(data)).Msg("File dump uploaded")
		}
	}
}

func buildStorageEnv(cfg api.StorageBackendConfig) map[string]string {
	env := map[string]string{
		"AWS_ACCESS_KEY_ID":     cfg.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY": cfg.SecretAccessKey,
	}
	if cfg.Region != "" {
		env["AWS_DEFAULT_REGION"] = cfg.Region
	}
	return env
}
