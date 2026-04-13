package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/pitr"
	"github.com/doobe01/nerdbackup-agent/internal/process"
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
	backupCounts map[string]int            // repo ID → backup count since last health check
	wsClient     *ws.Client                // optional WebSocket client for real-time progress
	cancelFuncs  map[string]context.CancelFunc // job ID → cancel function for running backups
	runningPIDs  map[string]int                // job ID → restic PID for pause/resume
	mu           sync.Mutex                    // protects cancelFuncs, runningPIDs, and lastRepos
	ctx          context.Context               // parent context for graceful shutdown
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
		cancelFuncs:  make(map[string]context.CancelFunc),
		runningPIDs:  make(map[string]int),
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
			s.mu.Lock()
			repos := s.lastRepos
			s.mu.Unlock()
			if len(repos) == 0 {
				// Force a config sync — repo might have just been created
				log.Info().Msg("No repos cached, forcing config sync")
				s.syncAndSchedule(s.ctx)
				s.mu.Lock()
				repos = s.lastRepos
				s.mu.Unlock()
			}
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

			ctx := s.ctx
			// Ensure repo is ready (init if needed, clear stale locks)
			if err := s.ensureRepoReady(ctx, *targetRepo); err != nil {
				log.Error().Err(err).Msg("Repo not ready — cannot start backup")
				if data.JobID != "" && s.wsClient != nil {
					_ = s.wsClient.Send(ws.Message{
						Type: "job_started",
						Data: map[string]string{"job_id": data.JobID, "status": "failed"},
					})
				}
				return
			}
			s.runBackup(ctx, *targetRepo, data.JobID)
		}()

	case "cancel":
		s.mu.Lock()
		// Resume first if paused (context cancellation needs running process)
		if pid, ok := s.runningPIDs[cmd.JobID]; ok {
			_ = process.ResumeProcess(pid)
		}
		if cancel, ok := s.cancelFuncs[cmd.JobID]; ok {
			log.Info().Msg("Cancelling backup")
			cancel()
			delete(s.cancelFuncs, cmd.JobID)
			delete(s.runningPIDs, cmd.JobID)
		} else {
			// Try cancelling all running backups if no specific job ID
			for id, cancel := range s.cancelFuncs {
				if pid, ok := s.runningPIDs[id]; ok {
					_ = process.ResumeProcess(pid)
				}
				log.Info().Str("cancelling_job", id).Msg("Cancelling backup")
				cancel()
				delete(s.cancelFuncs, id)
				delete(s.runningPIDs, id)
			}
		}
		s.mu.Unlock()

	case "pause":
		s.mu.Lock()
		pid, hasPID := s.runningPIDs[cmd.JobID]
		s.mu.Unlock()

		if !hasPID {
			// Try to pause any running backup if no specific job
			s.mu.Lock()
			for id, p := range s.runningPIDs {
				pid = p
				cmd.JobID = id
				hasPID = true
				break
			}
			s.mu.Unlock()
		}

		if hasPID {
			if err := process.SuspendProcess(pid); err != nil {
				log.Error().Err(err).Int("pid", pid).Msg("Failed to suspend process")
			} else {
				log.Info().Int("pid", pid).Msg("Backup paused")
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "job_started",
						Data: map[string]string{"job_id": cmd.JobID, "status": "paused"},
					})
				}
			}
		} else {
			log.Warn().Msg("No running backup to pause")
		}

	case "resume":
		s.mu.Lock()
		pid, hasPID := s.runningPIDs[cmd.JobID]
		s.mu.Unlock()

		if !hasPID {
			s.mu.Lock()
			for id, p := range s.runningPIDs {
				pid = p
				cmd.JobID = id
				hasPID = true
				break
			}
			s.mu.Unlock()
		}

		if hasPID {
			if err := process.ResumeProcess(pid); err != nil {
				log.Error().Err(err).Int("pid", pid).Msg("Failed to resume process")
			} else {
				log.Info().Int("pid", pid).Msg("Backup resumed")
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "job_started",
						Data: map[string]string{"job_id": cmd.JobID, "status": "running"},
					})
				}
			}
		} else {
			log.Warn().Msg("No paused backup to resume")
		}

	case "restore":
		var restoreData struct {
			JobID        string   `json:"jobId"`
			SnapshotID   string   `json:"snapshotId"`
			TargetPath   string   `json:"targetPath"`
			IncludePaths []string `json:"includePaths"`
			ExcludePaths []string `json:"excludePaths"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &restoreData)
		}

		go func() {
			repos := s.lastRepos
			if len(repos) == 0 {
				s.syncAndSchedule(s.ctx)
				repos = s.lastRepos
			}
			if len(repos) == 0 {
				log.Warn().Msg("No repos for restore command")
				return
			}

			targetRepo := &repos[0]
			runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))
			ctx := s.ctx

			log.Info().Str("snapshot", restoreData.SnapshotID).Str("target", restoreData.TargetPath).Msg("Starting restore via WebSocket")
			err := runner.Restore(ctx, restoreData.SnapshotID, restoreData.TargetPath, restoreData.IncludePaths, restoreData.ExcludePaths)

			report := api.JobReportRequest{
				RepoID:           targetRepo.ID,
				DashboardJobID:   restoreData.JobID,
				Operation:        "restore",
				StartedAt:        time.Now(),
				CompletedAt:      time.Now(),
				ResticSnapshotID: restoreData.SnapshotID,
			}
			if err != nil {
				report.Status = "failed"
				report.ErrorMessage = err.Error()
				log.Error().Err(err).Msg("Restore failed")
			} else {
				report.Status = "completed"
				log.Info().Str("target", restoreData.TargetPath).Msg("Restore completed")
			}

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.SendJobReport(report)
			} else {
				_ = s.client.ReportJob(ctx, report)
			}
		}()

	case "file_dump":
		var dumpData struct {
			RequestID  string `json:"requestId"`
			SnapshotID string `json:"snapshotId"`
			FilePath   string `json:"filePath"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &dumpData)
		}

		go func() {
			repos := s.lastRepos
			if len(repos) == 0 {
				s.syncAndSchedule(s.ctx)
				repos = s.lastRepos
			}
			if len(repos) == 0 {
				log.Warn().Msg("No repos for file_dump command")
				return
			}

			targetRepo := &repos[0]
			runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))
			ctx := s.ctx

			log.Info().Str("snapshot", dumpData.SnapshotID).Str("file", dumpData.FilePath).Msg("Dumping file via WebSocket")
			data, err := runner.Dump(ctx, dumpData.SnapshotID, dumpData.FilePath)
			if err != nil {
				log.Error().Err(err).Str("file", dumpData.FilePath).Msg("File dump failed")
				return
			}

			// Extract filename from path
			fileName := dumpData.FilePath
			for i := len(fileName) - 1; i >= 0; i-- {
				if fileName[i] == '/' || fileName[i] == '\\' {
					fileName = fileName[i+1:]
					break
				}
			}

			if err := s.client.UploadFileDump(ctx, dumpData.RequestID, data, fileName); err != nil {
				log.Error().Err(err).Str("requestId", dumpData.RequestID).Msg("Failed to upload file dump")
			} else {
				log.Info().Str("requestId", dumpData.RequestID).Int("size", len(data)).Msg("File dump uploaded")
			}
		}()

	case "forget_snapshot":
		var data struct {
			SnapshotID string `json:"snapshot_id"`
			RepoID     string `json:"repo_id"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}

		go func() {
			repos := s.lastRepos
			var targetRepo *api.RepoConfig
			for i := range repos {
				if repos[i].ID == data.RepoID {
					targetRepo = &repos[i]
					break
				}
			}
			if targetRepo == nil && len(repos) > 0 {
				targetRepo = &repos[0]
			}
			if targetRepo == nil {
				log.Warn().Msg("No repo for forget_snapshot command")
				return
			}

			runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))
			ctx := s.ctx

			// Forget the specific snapshot
			if data.SnapshotID != "" {
				if err := runner.ForgetSnapshot(ctx, data.SnapshotID); err != nil {
					log.Error().Err(err).Str("snapshot", data.SnapshotID).Msg("restic forget failed")
					return
				}
				log.Info().Str("snapshot", data.SnapshotID).Msg("Snapshot forgotten")
			}

			// Prune unreferenced data
			if err := runner.Prune(ctx); err != nil {
				log.Error().Err(err).Msg("restic prune failed")
			} else {
				log.Info().Msg("Prune completed — storage reclaimed")
			}

			// Report back to server
			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "forget_completed",
					Data: map[string]string{
						"snapshot_id": data.SnapshotID,
						"repo_id":    data.RepoID,
					},
				})
			}
		}()

	case "fs_list":
		var fsData struct {
			RequestID string `json:"request_id"`
			Path      string `json:"path"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &fsData)
		}

		go func() {
			path := fsData.Path
			if path == "" {
				// Default: show root drives on Windows, / on Unix
				if runtime.GOOS == "windows" {
					// List drive letters
					entries := []map[string]interface{}{}
					for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
						root := string(drive) + ":\\"
						if _, err := os.Stat(root); err == nil {
							entries = append(entries, map[string]interface{}{
								"name": root,
								"type": "dir",
								"size": 0,
							})
						}
					}
					if s.wsClient != nil && s.wsClient.IsConnected() {
						_ = s.wsClient.Send(ws.Message{
							Type: "fs_list_response",
							Data: map[string]interface{}{
								"request_id": fsData.RequestID,
								"path":       "",
								"entries":    entries,
							},
						})
					}
					return
				}
				path = "/"
			}

			dirEntries, err := os.ReadDir(path)
			if err != nil {
				log.Error().Err(err).Str("path", path).Msg("fs_list failed")
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "fs_list_response",
						Data: map[string]interface{}{
							"request_id": fsData.RequestID,
							"path":       path,
							"error":      err.Error(),
							"entries":    []interface{}{},
						},
					})
				}
				return
			}

			entries := []map[string]interface{}{}
			for _, e := range dirEntries {
				entryType := "file"
				if e.IsDir() {
					entryType = "dir"
				}
				size := int64(0)
				if info, infoErr := e.Info(); infoErr == nil {
					size = info.Size()
				}
				entries = append(entries, map[string]interface{}{
					"name": e.Name(),
					"type": entryType,
					"size": size,
				})
			}

			// Sort: directories first, then files, alphabetical within each
			sort.Slice(entries, func(i, j int) bool {
				ti := entries[i]["type"].(string)
				tj := entries[j]["type"].(string)
				if ti != tj {
					return ti == "dir"
				}
				return entries[i]["name"].(string) < entries[j]["name"].(string)
			})

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "fs_list_response",
					Data: map[string]interface{}{
						"request_id": fsData.RequestID,
						"path":       path,
						"entries":    entries,
					},
				})
			}
		}()

	case "fs_search":
		var searchData struct {
			RequestID string `json:"request_id"`
			Path      string `json:"path"`
			Pattern   string `json:"pattern"`
			MaxDepth  int    `json:"max_depth"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &searchData)
		}

		go func() {
			roots := []string{}
			if searchData.Path != "" {
				roots = append(roots, searchData.Path)
			} else if runtime.GOOS == "windows" {
				// Search all available drives
				for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
					r := string(drive) + ":\\"
					if _, err := os.Stat(r); err == nil {
						roots = append(roots, r)
					}
				}
			} else {
				roots = append(roots, "/")
			}
			maxDepth := searchData.MaxDepth
			if maxDepth <= 0 {
				maxDepth = 8
			}
			pattern := strings.ToLower(searchData.Pattern)
			if pattern == "" {
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "fs_search_response",
						Data: map[string]interface{}{
							"request_id": searchData.RequestID,
							"error":      "search pattern is required",
							"results":    []interface{}{},
						},
					})
				}
				return
			}

			var results []map[string]interface{}
			maxResults := 50

			var walk func(dir string, depth int)
			walk = func(dir string, depth int) {
				if depth > maxDepth || len(results) >= maxResults {
					return
				}
				dirEntries, err := os.ReadDir(dir)
				if err != nil {
					return // skip unreadable directories
				}
				for _, e := range dirEntries {
					if len(results) >= maxResults {
						return
					}
					name := e.Name()
					fullPath := filepath.Join(dir, name)
					if strings.Contains(strings.ToLower(name), pattern) {
						entryType := "file"
						if e.IsDir() {
							entryType = "dir"
						}
						size := int64(0)
						if info, infoErr := e.Info(); infoErr == nil {
							size = info.Size()
						}
						results = append(results, map[string]interface{}{
							"name": name,
							"path": fullPath,
							"type": entryType,
							"size": size,
						})
					}
					if e.IsDir() {
						walk(fullPath, depth+1)
					}
				}
			}

			for _, root := range roots {
				if len(results) >= maxResults {
					break
				}
				walk(root, 0)
			}

			rootDisplay := searchData.Path
			if rootDisplay == "" {
				rootDisplay = "all drives"
			}

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "fs_search_response",
					Data: map[string]interface{}{
						"request_id": searchData.RequestID,
						"root":       rootDisplay,
						"pattern":    searchData.Pattern,
						"results":    results,
						"truncated":  len(results) >= maxResults,
					},
				})
			}
		}()

	case "fs_mkdir":
		var mkdirData struct {
			RequestID string `json:"request_id"`
			Path      string `json:"path"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &mkdirData)
		}

		go func() {
			path := mkdirData.Path
			if path == "" {
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "fs_mkdir_response",
						Data: map[string]interface{}{
							"request_id": mkdirData.RequestID,
							"path":       path,
							"success":    false,
							"error":      "path is required",
						},
					})
				}
				return
			}

			err := os.MkdirAll(path, 0755)
			if err != nil {
				log.Error().Err(err).Str("path", path).Msg("fs_mkdir failed")
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "fs_mkdir_response",
						Data: map[string]interface{}{
							"request_id": mkdirData.RequestID,
							"path":       path,
							"success":    false,
							"error":      err.Error(),
						},
					})
				}
				return
			}

			log.Info().Str("path", path).Msg("fs_mkdir succeeded")
			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "fs_mkdir_response",
					Data: map[string]interface{}{
						"request_id": mkdirData.RequestID,
						"path":       path,
						"success":    true,
					},
				})
			}
		}()

	case "config_update":
		// Force an immediate config re-sync
		log.Info().Msg("Config update command received, triggering re-sync")
		go func() {
			ctx := s.ctx
			s.syncAndSchedule(ctx)
		}()

	case "pitr_setup":
		var data struct {
			ConfigID           string `json:"config_id"`
			DatabaseType       string `json:"database_type"`
			ConnectionHost     string `json:"connection_host"`
			ConnectionPort     int    `json:"connection_port"`
			DatabaseName       string `json:"database_name"`
			User               string `json:"user"`
			Password           string `json:"password"`
			WALArchiveDir      string `json:"wal_archive_dir"`
			BaseBackupDir      string `json:"base_backup_dir"`
			WALArchiveInterval int    `json:"wal_archive_interval"`
			BaseBackupCron     string `json:"base_backup_cron"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}

		go func() {
			cfg := pitr.PITRConfig{
				DatabaseType:       data.DatabaseType,
				ConnectionHost:     data.ConnectionHost,
				ConnectionPort:     data.ConnectionPort,
				DatabaseName:       data.DatabaseName,
				User:               data.User,
				Password:           data.Password,
				WALArchiveDir:      data.WALArchiveDir,
				BaseBackupDir:      data.BaseBackupDir,
				WALArchiveInterval: data.WALArchiveInterval,
				BaseBackupCron:     data.BaseBackupCron,
			}

			result := api.PITRSetupResult{
				ConfigID: data.ConfigID,
			}

			configLines, archiveDir, err := pitr.SetupPostgresWAL(cfg)
			if err != nil {
				log.Error().Err(err).Msg("PITR setup failed")
				result.Status = "failed"
				result.Error = err.Error()
			} else {
				log.Info().Str("archive_dir", archiveDir).Msg("PITR setup completed")
				result.Status = "success"
				result.ConfigLines = configLines
				result.ArchiveDir = archiveDir
			}

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "pitr_setup_result",
					Data: result,
				})
			}
		}()

	case "pitr_base_backup":
		var data struct {
			ConfigID       string `json:"config_id"`
			DatabaseType   string `json:"database_type"`
			ConnectionHost string `json:"connection_host"`
			ConnectionPort int    `json:"connection_port"`
			DatabaseName   string `json:"database_name"`
			User           string `json:"user"`
			Password       string `json:"password"`
			WALArchiveDir  string `json:"wal_archive_dir"`
			BaseBackupDir  string `json:"base_backup_dir"`
			RepoID         string `json:"repo_id"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}

		go func() {
			cfg := pitr.PITRConfig{
				DatabaseType:   data.DatabaseType,
				ConnectionHost: data.ConnectionHost,
				ConnectionPort: data.ConnectionPort,
				DatabaseName:   data.DatabaseName,
				User:           data.User,
				Password:       data.Password,
				WALArchiveDir:  data.WALArchiveDir,
				BaseBackupDir:  data.BaseBackupDir,
			}

			startedAt := time.Now()
			result := api.PITRBaseBackupResult{
				ConfigID:  data.ConfigID,
				StartedAt: startedAt.Format(time.RFC3339),
			}

			ctx := s.ctx

			// Step 1: Run pg_basebackup
			log.Info().Str("host", cfg.ConnectionHost).Str("db", cfg.DatabaseName).Msg("Starting PITR base backup")
			backupDir, err := pitr.RunBaseBackup(ctx, cfg)
			if err != nil {
				log.Error().Err(err).Msg("PITR base backup failed")
				result.Status = "failed"
				result.Error = err.Error()
				result.CompletedAt = time.Now().Format(time.RFC3339)
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "pitr_base_backup_result",
						Data: result,
					})
				}
				return
			}
			result.BackupDir = backupDir

			// Step 2: Upload base backup to S3 via restic
			var targetRepo *api.RepoConfig
			repos := s.lastRepos
			for i := range repos {
				if data.RepoID != "" && repos[i].ID == data.RepoID {
					targetRepo = &repos[i]
					break
				}
			}
			if targetRepo == nil && len(repos) > 0 {
				targetRepo = &repos[0]
			}

			if targetRepo != nil {
				runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))

				// Ensure repo is ready
				if initErr := s.ensureRepoReady(ctx, *targetRepo); initErr != nil {
					log.Warn().Err(initErr).Msg("Repo not ready for PITR base backup upload")
				}

				snapshotID, uploadErr := pitr.UploadBaseBackup(ctx, runner, backupDir, cfg)
				if uploadErr != nil {
					log.Error().Err(uploadErr).Msg("PITR base backup upload to S3 failed")
					result.Status = "failed"
					result.Error = fmt.Sprintf("backup succeeded but upload failed: %s", uploadErr.Error())
				} else {
					result.Status = "completed"
					result.ResticSnapshotID = snapshotID
					log.Info().Str("snapshot_id", snapshotID).Msg("PITR base backup uploaded to S3")
				}
			} else {
				// No repo configured — backup is local only
				result.Status = "completed"
				log.Warn().Msg("No restic repo configured — base backup stored locally only")
			}

			result.CompletedAt = time.Now().Format(time.RFC3339)

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "pitr_base_backup_result",
					Data: result,
				})
			}
		}()

	case "pitr_restore":
		var data struct {
			ConfigID       string `json:"config_id"`
			DatabaseType   string `json:"database_type"`
			ConnectionHost string `json:"connection_host"`
			ConnectionPort int    `json:"connection_port"`
			DatabaseName   string `json:"database_name"`
			User           string `json:"user"`
			Password       string `json:"password"`
			WALArchiveDir  string `json:"wal_archive_dir"`
			BaseBackupDir  string `json:"base_backup_dir"`
			TargetTime     string `json:"target_time"`
			RestoreDir     string `json:"restore_dir"`
			RepoID         string `json:"repo_id"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}

		go func() {
			cfg := pitr.PITRConfig{
				DatabaseType:   data.DatabaseType,
				ConnectionHost: data.ConnectionHost,
				ConnectionPort: data.ConnectionPort,
				DatabaseName:   data.DatabaseName,
				User:           data.User,
				Password:       data.Password,
				WALArchiveDir:  data.WALArchiveDir,
				BaseBackupDir:  data.BaseBackupDir,
			}

			startedAt := time.Now()
			result := api.PITRRestoreResult{
				ConfigID:   data.ConfigID,
				TargetTime: data.TargetTime,
				RestoreDir: data.RestoreDir,
				StartedAt:  startedAt.Format(time.RFC3339),
			}

			ctx := s.ctx

			// Parse target time
			targetTime, parseErr := time.Parse(time.RFC3339, data.TargetTime)
			if parseErr != nil {
				log.Error().Err(parseErr).Str("target_time", data.TargetTime).Msg("Invalid target time for PITR restore")
				result.Status = "failed"
				result.Error = fmt.Sprintf("invalid target_time: %s", parseErr.Error())
				result.CompletedAt = time.Now().Format(time.RFC3339)
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "pitr_restore_result",
						Data: result,
					})
				}
				return
			}

			restoreDir := data.RestoreDir
			if restoreDir == "" {
				restoreDir = fmt.Sprintf("/var/lib/nerdbackup/pitr-restore/%s", time.Now().Format("20060102-150405"))
			}
			result.RestoreDir = restoreDir

			// Find repo for restic restore
			var targetRepo *api.RepoConfig
			repos := s.lastRepos
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
				log.Error().Msg("No restic repo configured for PITR restore")
				result.Status = "failed"
				result.Error = "no restic repo configured"
				result.CompletedAt = time.Now().Format(time.RFC3339)
				if s.wsClient != nil && s.wsClient.IsConnected() {
					_ = s.wsClient.Send(ws.Message{
						Type: "pitr_restore_result",
						Data: result,
					})
				}
				return
			}

			runner := restic.NewRunner(s.resticBinary, targetRepo.ResticRepoPath, targetRepo.ResticPassword, buildStorageEnv(targetRepo.StorageConfig))

			log.Info().
				Str("target_time", targetTime.Format(time.RFC3339)).
				Str("restore_dir", restoreDir).
				Msg("Starting PITR restore")

			if err := pitr.RestoreToPoint(ctx, cfg, runner, targetTime, restoreDir); err != nil {
				log.Error().Err(err).Msg("PITR restore failed")
				result.Status = "failed"
				result.Error = err.Error()
			} else {
				log.Info().Msg("PITR restore completed")
				result.Status = "completed"
			}

			result.CompletedAt = time.Now().Format(time.RFC3339)

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "pitr_restore_result",
					Data: result,
				})
			}
		}()

	case "pitr_status":
		var data struct {
			ConfigID       string `json:"config_id"`
			DatabaseType   string `json:"database_type"`
			ConnectionHost string `json:"connection_host"`
			ConnectionPort int    `json:"connection_port"`
			DatabaseName   string `json:"database_name"`
			User           string `json:"user"`
			Password       string `json:"password"`
			WALArchiveDir  string `json:"wal_archive_dir"`
			BaseBackupDir  string `json:"base_backup_dir"`
		}
		if cmd.Data != nil {
			_ = json.Unmarshal(cmd.Data, &data)
		}

		go func() {
			cfg := pitr.PITRConfig{
				DatabaseType:   data.DatabaseType,
				ConnectionHost: data.ConnectionHost,
				ConnectionPort: data.ConnectionPort,
				DatabaseName:   data.DatabaseName,
				User:           data.User,
				Password:       data.Password,
				WALArchiveDir:  data.WALArchiveDir,
				BaseBackupDir:  data.BaseBackupDir,
			}

			walStatus := pitr.GetWALStatus(cfg)

			report := api.PITRStatusReport{
				ConfigID:        data.ConfigID,
				DatabaseType:    cfg.DatabaseType,
				DatabaseName:    cfg.DatabaseName,
				ConnectionHost:  cfg.ConnectionHost,
				WALCount:        walStatus.WALArchiveCount,
				WALSizeBytes:    walStatus.ArchiveDirSizeBytes,
				CurrentRPOSec:   walStatus.CurrentRPOSeconds,
				LastWALArchived: walStatus.LastWALArchived,
				LastBaseBackup:  walStatus.LastBaseBackup,
				ArchiveDir:      cfg.WALArchiveDir,
				Status:          "active",
			}

			if report.ArchiveDir == "" {
				report.ArchiveDir = "/var/lib/nerdbackup/wal-archive"
			}

			// Check if WAL archiving appears healthy
			if walStatus.WALArchiveCount == 0 {
				report.Status = "not_configured"
			} else if walStatus.CurrentRPOSeconds > 3600 {
				// RPO > 1 hour suggests archiving may be stalled
				report.Status = "error"
				report.ErrorMessage = fmt.Sprintf("WAL archiving may be stalled: last archive was %d seconds ago", walStatus.CurrentRPOSeconds)
			}

			log.Info().
				Int("wal_count", report.WALCount).
				Int("rpo_seconds", report.CurrentRPOSec).
				Str("status", report.Status).
				Msg("PITR status collected")

			if s.wsClient != nil && s.wsClient.IsConnected() {
				_ = s.wsClient.Send(ws.Message{
					Type: "pitr_status_result",
					Data: report,
				})
			}
		}()

	default:
		log.Warn().Msg("Unknown command action")
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) {
	s.ctx = ctx // store for goroutines spawned by HandleCommand
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

		// Ensure all repos are ready (init if needed, clear stale locks)
		for _, repo := range repos {
			if err := s.ensureRepoReady(ctx, repo); err != nil {
				logging.Log.Warn().Err(err).Str("repo", repo.ID).Msg("Repo not ready during sync — will retry before backup")
			}
		}

		// Rebuild cron entries
		for _, entry := range s.cron.Entries() {
			s.cron.Remove(entry.ID)
		}

		// Only schedule local cron as fallback when WebSocket is NOT connected.
		// When WS is connected, the server scheduler sends start_backup commands
		// with a dashboard_job_id, avoiding orphan jobs.
		if s.wsClient == nil || !s.wsClient.IsConnected() {
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
		} else {
			logging.Log.Debug().Msg("WebSocket connected — server scheduler handles cron, skipping local cron")
		}

		s.mu.Lock()
		s.lastRepos = repos
		s.mu.Unlock()
	}

	// Only check polling queues when WebSocket is NOT connected.
	// When WS is connected, commands arrive instantly — no need to poll.
	if s.wsClient == nil || !s.wsClient.IsConnected() {
		s.mu.Lock()
		activeRepos := s.lastRepos
		s.mu.Unlock()
		if len(activeRepos) == 0 {
			activeRepos = repos
		}
		s.checkPendingRestores(ctx, activeRepos)
		s.checkPendingBackups(ctx, activeRepos)
		s.checkPendingFileDumps(ctx, activeRepos)
	}
}

// ensureRepoReady verifies the restic repo exists and is accessible.
// If not, it initializes it. Called before every backup — never trust local state.
func (s *Scheduler) ensureRepoReady(ctx context.Context, repo api.RepoConfig) error {
	storageEnv := buildStorageEnv(repo.StorageConfig)
	runner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

	// Always clear stale locks first
	_ = runner.UnlockIfStale(ctx, 0) // force unlock — we're about to use this repo

	// Verify repo exists by running `restic snapshots` (fast, <1s)
	if runner.IsInitialized(ctx) {
		return nil
	}

	// Repo not accessible — init it
	logging.Log.Info().Str("repo", repo.ID).Msg("Initializing restic repo")
	if err := runner.Init(ctx); err != nil {
		return fmt.Errorf("restic init failed: %w", err)
	}

	// Verify init succeeded
	if !runner.IsInitialized(ctx) {
		return fmt.Errorf("restic init completed but repo still not accessible")
	}

	logging.Log.Info().Str("repo", repo.ID).Msg("Repo initialized and verified")
	return nil
}

func (s *Scheduler) runBackup(ctx context.Context, repo api.RepoConfig, dashboardJobID ...string) {
	log := logging.Log.With().Str("repo", repo.ID).Logger()
	log.Info().Strs("paths", repo.Paths).Msg("Starting scheduled backup")

	djID := ""
	if len(dashboardJobID) > 0 {
		djID = dashboardJobID[0]
	}

	// Create cancelable context for this backup (so cancel command can stop it)
	backupCtx, backupCancel := context.WithTimeout(ctx, 24*time.Hour)
	defer backupCancel()

	if djID != "" {
		s.mu.Lock()
		s.cancelFuncs[djID] = backupCancel
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.cancelFuncs, djID)
			delete(s.runningPIDs, djID)
			s.mu.Unlock()
		}()
	}

	// Notify server that backup has started (so dashboard shows "running")
	if s.wsClient != nil && s.wsClient.IsConnected() {
		_ = s.wsClient.Send(ws.Message{
			Type: "job_started",
			Data: map[string]string{
				"job_id":  djID,
				"repo_id": repo.ID,
				"status":  "running",
			},
		})
	}

	// Ensure repo is ready before backup
	if err := s.ensureRepoReady(backupCtx, repo); err != nil {
		log.Error().Err(err).Msg("Repo not ready — cannot start backup")
		if djID != "" && s.wsClient != nil {
			_ = s.wsClient.Send(ws.Message{
				Type: "job_started",
				Data: map[string]string{"job_id": djID, "status": "failed"},
			})
		}
		return
	}

	// Run pre-backup hook
	if err := runPreHook(backupCtx, repo.PreBackupCommand); err != nil {
		log.Error().Err(err).Msg("Pre-backup hook failed — skipping backup")
		return
	}

	startedAt := time.Now()
	storageEnv := buildStorageEnv(repo.StorageConfig)
	runner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

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
	summary, err := runner.Backup(backupCtx, restic.BackupOptions{
		Paths:             repo.Paths,
		Excludes:          excludes,
		Tags:              tags,
		BandwidthLimitKiB: repo.BandwidthLimitKiB,
		OnStarted: func(pid int) {
			log.Info().Int("pid", pid).Msg("Restic process started")
			if djID != "" {
				s.mu.Lock()
				s.runningPIDs[djID] = pid
				s.mu.Unlock()
			}
		},
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

	// If cancelled, unlock the repo (restic leaves a lock when killed)
	if backupCtx.Err() == context.Canceled {
		unlockRunner := restic.NewRunner(s.resticBinary, repo.ResticRepoPath, repo.ResticPassword, buildStorageEnv(repo.StorageConfig))
		if unlockErr := unlockRunner.UnlockIfStale(context.Background(), 0); unlockErr != nil {
			log.Warn().Err(unlockErr).Msg("Failed to unlock repo after cancel")
		} else {
			log.Info().Msg("Repo unlocked after cancel")
		}
	}

	// Build job report (djID already set at function start)

	report := api.JobReportRequest{
		RepoID:         repo.ID,
		PolicyID:       repo.PolicyID,
		DashboardJobID: djID,
		Operation:      "backup",
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
	}

	if err != nil {
		if backupCtx.Err() == context.Canceled {
			report.Status = "cancelled"
			log.Info().Msg("Backup cancelled by user")
		} else {
			report.Status = "failed"
			report.ErrorMessage = err.Error()
			log.Error().Err(err).Msg("Backup failed")
		}
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
		if files, lsErr := runner.LsFiles(backupCtx, summary.SnapshotID, 500); lsErr == nil && len(files) > 0 {
			fileList := make([]map[string]interface{}, len(files))
			for i, f := range files {
				fileList[i] = map[string]interface{}{"path": f.Path, "size": f.Size, "modified_at": f.ModifiedAt}
			}
			report.Files = fileList
		}

		// Get real dedup/compression stats from restic
		if sizeStats, statsErr := runner.SizeStats(backupCtx); statsErr == nil {
			report.Stats.RepoRawSize = sizeStats.RawSize
			report.Stats.RepoRestoreSize = sizeStats.RestoreSize
			log.Info().Int64("raw", sizeStats.RawSize).Int64("restore", sizeStats.RestoreSize).Msg("Repo size stats")
		} else {
			log.Warn().Err(statsErr).Msg("Failed to get repo size stats")
		}

		// Update last backup time
		s.cfg.LastBackupAt = completedAt.Format(time.RFC3339)
		_ = config.Save(s.cfg)
	}

	// Skip reporting if cancelled — dashboard already set the status
	if report.Status == "cancelled" {
		log.Info().Msg("Skipping job report for cancelled backup — dashboard handles status")
	} else {
		// Report to API — prefer WebSocket (instant), fall back to HTTP (with retry + pending persistence)
		if s.wsClient != nil && s.wsClient.IsConnected() {
			if wsErr := s.wsClient.SendJobReport(report); wsErr != nil {
				log.Warn().Err(wsErr).Msg("WebSocket job report failed, falling back to HTTP")
				_ = s.client.ReportJob(ctx, report)
			}
		} else {
			_ = s.client.ReportJob(ctx, report)
		}
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
