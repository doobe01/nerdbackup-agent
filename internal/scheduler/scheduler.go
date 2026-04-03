package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
	"github.com/robfig/cron/v3"
)

// Scheduler evaluates repo schedules and triggers restic backups.
type Scheduler struct {
	client        *api.Client
	resticBinary  string
	cron          *cron.Cron
	lastRepos     []api.RepoConfig
	syncInterval  time.Duration
}

// New creates a scheduler.
func New(client *api.Client, resticBinary string, syncInterval time.Duration) *Scheduler {
	return &Scheduler{
		client:       client,
		resticBinary: resticBinary,
		cron:         cron.New(),
		syncInterval: syncInterval,
	}
}

// Start begins the scheduler loop: syncs config and manages cron entries.
func (s *Scheduler) Start(ctx context.Context) {
	s.syncAndSchedule()
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
			s.syncAndSchedule()
		}
	}
}

func (s *Scheduler) syncAndSchedule() {
	repos, err := s.client.GetRepos()
	if err != nil {
		logging.Log.Warn().Err(err).Msg("Failed to sync repo config")
		return
	}

	logging.Log.Debug().Int("repos", len(repos)).Msg("Config synced")

	// Rebuild cron entries if config changed
	if !reposEqual(s.lastRepos, repos) {
		logging.Log.Info().Int("repos", len(repos)).Msg("Config changed, rebuilding schedules")

		// Clear existing entries
		for _, entry := range s.cron.Entries() {
			s.cron.Remove(entry.ID)
		}

		for _, repo := range repos {
			if repo.ScheduleCron == "" {
				continue
			}
			r := repo // capture
			_, err := s.cron.AddFunc(r.ScheduleCron, func() {
				s.runBackup(r)
			})
			if err != nil {
				logging.Log.Error().Err(err).Str("repo", r.ID).Str("cron", r.ScheduleCron).Msg("Invalid cron expression")
			}
		}

		s.lastRepos = repos
	}
}

func (s *Scheduler) runBackup(repo api.RepoConfig) {
	log := logging.Log.With().Str("repo", repo.ID).Logger()
	log.Info().Strs("paths", repo.Paths).Msg("Starting scheduled backup")

	startedAt := time.Now()

	// Build restic runner
	storageEnv := buildStorageEnv(repo.StorageConfig)
	runner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPasswordEncrypted, storageEnv)

	ctx := context.Background()

	// Run backup
	summary, err := runner.Backup(ctx, repo.Paths, repo.ExcludePatterns, repo.Tags, func(p restic.ProgressEntry) {
		log.Debug().Float64("percent", p.PercentDone*100).Int64("bytes", p.BytesDone).Msg("Progress")
	})

	completedAt := time.Now()

	// Report to API
	report := api.JobReportRequest{
		RepoID:      repo.ID,
		PolicyID:    repo.PolicyID,
		Operation:   "backup",
		StartedAt:   startedAt,
		CompletedAt: completedAt,
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
	}

	if reportErr := s.client.ReportJob(report); reportErr != nil {
		log.Error().Err(reportErr).Msg("Failed to report job to API")
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

func reposEqual(a, b []api.RepoConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].ScheduleCron != b[i].ScheduleCron || fmt.Sprintf("%v", a[i].Paths) != fmt.Sprintf("%v", b[i].Paths) {
			return false
		}
	}
	return true
}
