package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/api"
	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/docker"
	"github.com/doobe01/nerdbackup-agent/internal/doctor"
	"github.com/doobe01/nerdbackup-agent/internal/heartbeat"
	"github.com/doobe01/nerdbackup-agent/internal/localapi"
	"github.com/doobe01/nerdbackup-agent/internal/logging"
	"github.com/doobe01/nerdbackup-agent/internal/restic"
	"github.com/doobe01/nerdbackup-agent/internal/scheduler"
	"github.com/doobe01/nerdbackup-agent/internal/service"
	"github.com/doobe01/nerdbackup-agent/internal/tray"
	"github.com/doobe01/nerdbackup-agent/internal/updater"
	"github.com/doobe01/nerdbackup-agent/internal/ws"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	// CRITICAL: Detect Windows Service mode BEFORE Cobra.
	// Windows Services have no console — Cobra writing to stderr causes failures.
	// Professional agents (Tailscale, Datadog) all do this.
	if service.IsWindowsService() {
		runAsWindowsService()
		return
	}

	// Interactive mode — Cobra handles everything
	root := &cobra.Command{
		Use:     "nerdbackup-agent",
		Short:   "NerdBackup Agent — Restic-powered backup for local files",
		Version: version,
	}

	root.AddCommand(initCmd())
	root.AddCommand(runCmd())
	root.AddCommand(backupCmd())
	root.AddCommand(restoreCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(snapshotsCmd())
	root.AddCommand(doctorCmd())
	root.AddCommand(updateCmd())
	root.AddCommand(uninstallCmd())
	root.AddCommand(installServiceCmd())
	root.AddCommand(serviceCmd())
	root.AddCommand(dockerDiscoverCmd())
	root.AddCommand(trayCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runAsWindowsService runs the agent directly as a Windows Service.
// Skips Cobra entirely — no stderr, no console output.
func runAsWindowsService() {
	logging.Init(false, true) // isService=true → file only, no stderr
	logging.Log.Info().Str("version", version).Msg("Starting as Windows Service")

	cfg, err := config.Load()
	if err != nil {
		logging.Log.Error().Err(err).Msg("Failed to load config — service cannot start")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.RunAsService(
		func() error { return runAgent(ctx, cfg) },
		func() { cancel() },
	); err != nil {
		logging.Log.Error().Err(err).Msg("Service exited with error")
	}
}

func initCmd() *cobra.Command {
	var apiKey string
	var installToken string
	var apiURL string
	var name string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register this agent with NerdBackup",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)

			if config.Exists() {
				return fmt.Errorf("agent already initialized — config at %s", config.ConfigPath())
			}

			if apiURL == "" {
				apiURL = "https://nerdbackup.com"
			}

			hostname, _ := os.Hostname()
			if name == "" {
				name = hostname
			}

			var agentID, agentToken string

			if installToken != "" {
				// Register using pre-authenticated install token (zero-touch flow)
				logging.Log.Info().Str("api", apiURL).Str("name", name).Msg("Registering agent with install token")

				result, err := api.RegisterWithToken(apiURL, installToken, api.RegisterAgentRequest{
					Name:     name,
					Platform: runtime.GOOS,
					Arch:     runtime.GOARCH,
					Hostname: hostname,
				})
				if err != nil {
					return fmt.Errorf("registration failed: %w", err)
				}
				agentID = result.AgentID
				agentToken = result.AgentToken
				if result.APIBaseURL != "" {
					apiURL = result.APIBaseURL
				}
			} else if apiKey != "" {
				// Register using API key (manual flow)
				logging.Log.Info().Str("api", apiURL).Str("name", name).Msg("Registering agent")

				result, err := api.Register(apiURL, apiKey, api.RegisterAgentRequest{
					Name:     name,
					Platform: runtime.GOOS,
					Arch:     runtime.GOARCH,
					Hostname: hostname,
				})
				if err != nil {
					return fmt.Errorf("registration failed: %w", err)
				}
				agentID = result.ID
				agentToken = result.Token
			} else {
				return fmt.Errorf("either --api-key or --install-token is required")
			}

			cfg := &config.AgentConfig{
				AgentID:    agentID,
				AgentToken: agentToken,
				APIBaseURL: apiURL,
				Name:       name,
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			logging.Log.Info().
				Str("agent_id", agentID).
				Str("config", config.ConfigPath()).
				Msg("Agent registered successfully")

			resticPath, err := restic.FindOrInstall()
			if err != nil {
				logging.Log.Warn().Err(err).Msg("Restic not found — install it manually or run 'nerdbackup-agent run' to auto-install")
			} else {
				logging.Log.Info().Str("restic", resticPath).Msg("Restic ready")
			}

			// Auto-install service when using install token (zero-touch flow)
			if installToken != "" {
				binaryPath, binErr := os.Executable()
				if binErr != nil {
					logging.Log.Warn().Err(binErr).Msg("Could not determine binary path for service install")
				} else {
					logging.Log.Info().Msg("Auto-installing as system service")
					if svcErr := service.Install(binaryPath); svcErr != nil {
						logging.Log.Warn().Err(svcErr).Msg("Failed to auto-install service — you can run 'nerdbackup-agent install-service' manually")
					} else {
						logging.Log.Info().Msg("Service installed and started")
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "NerdBackup API key")
	cmd.Flags().StringVar(&installToken, "install-token", "", "Pre-authenticated install token (from dashboard)")
	cmd.Flags().StringVar(&apiURL, "api-url", "https://nerdbackup.com", "NerdBackup API base URL")
	cmd.Flags().StringVar(&name, "name", "", "Agent name (defaults to hostname)")

	return cmd
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the agent (heartbeat + scheduler)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Interactive mode only (Windows Service goes through runAsWindowsService)
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("not initialized — run 'nerdbackup-agent init' first: %w", err)
			}

			logging.Init(cfg.Debug, false) // interactive mode — stderr + file

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

			go func() {
				<-sigCh
				logging.Log.Info().Msg("Shutting down agent gracefully...")
				cancel()
			}()

			return runAgent(ctx, cfg)
		},
	}
}

func runAgent(ctx context.Context, cfg *config.AgentConfig) error {
	startedAt := time.Now()
	cfg.StartedAt = startedAt
	_ = config.Save(cfg)

	logging.Log.Info().Str("agent_id", cfg.AgentID).Str("version", version).Msg("Starting NerdBackup Agent")

	resticBinary, err := restic.FindOrInstall()
	if err != nil {
		return fmt.Errorf("restic not available: %w", err)
	}

	resticVersion := getResticVersion(resticBinary)
	client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)

	// Start scheduler
	sched := scheduler.New(client, resticBinary, cfg.AgentID, cfg, 5*time.Minute)

	// Start WebSocket client for real-time communication
	wsClient := ws.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken, func(cmd ws.Command) {
		sched.HandleCommand(cmd)
	})
	go wsClient.Run(ctx)

	// Expose WS client to scheduler for progress streaming
	sched.SetWSClient(wsClient)

	// Set version info for local API status endpoint
	sched.SetVersionInfo(version, resticVersion)

	go sched.Start(ctx)

	// Start local HTTP API server for system tray and other local tools
	localServer := localapi.New(sched)
	localServer.Start(ctx)

	// Launch system tray if a desktop environment is available
	if hasDisplay() {
		go func() {
			logging.Log.Info().Msg("Desktop detected — launching system tray")
			tray.Run(version)
		}()
	} else {
		logging.Log.Debug().Msg("No desktop environment — skipping system tray")
	}

	// Heartbeat: send over WebSocket when connected, fall back to HTTP
	go func() {
		hostname, _ := os.Hostname()

		// Send initial heartbeat immediately
		sendHeartbeat(ctx, wsClient, client, version, resticVersion, startedAt, hostname)

		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logging.Log.Info().Msg("Heartbeat stopped")
				return
			case <-ticker.C:
				sendHeartbeat(ctx, wsClient, client, version, resticVersion, startedAt, hostname)
			}
		}
	}()

	// Start auto-updater (checks every hour, downloads + swaps binary if new version)
	autoUpdater := updater.New(version)
	go func() {
		// Initial check after 30 seconds
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if autoUpdater.CheckAndUpdate(ctx) {
					logging.Log.Info().Msg("Auto-update installed — restarting service")
					// On Windows, the service manager will restart us.
					// On Linux/macOS, systemd/launchd will restart us.
					os.Exit(0)
				}
				timer.Reset(5 * time.Minute)
			}
		}
	}()

	logging.Log.Info().Msg("Agent running")

	<-ctx.Done()
	wsClient.Close()
	time.Sleep(2 * time.Second)
	logging.Log.Info().Msg("Agent stopped")
	return nil
}

// sendHeartbeat sends a heartbeat over WebSocket if connected, otherwise falls back to HTTP.
func sendHeartbeat(ctx context.Context, wsClient *ws.Client, httpClient *api.Client, agentVersion, resticVersion string, startedAt time.Time, hostname string) {
	if wsClient.IsConnected() {
		err := wsClient.SendHeartbeat(ws.HeartbeatData{
			AgentVersion:  agentVersion,
			ResticVersion: resticVersion,
			Platform:      runtime.GOOS,
			Arch:          runtime.GOARCH,
			Hostname:      hostname,
			UptimeSeconds: int64(time.Since(startedAt).Seconds()),
			CPUCount:      runtime.NumCPU(),
			MemTotalBytes: heartbeat.GetTotalMemory(),
			DiskFreeBytes: heartbeat.GetFreeDisk(),
		})
		if err == nil {
			logging.Log.Debug().Msg("Heartbeat sent via WebSocket")
			return
		}
		logging.Log.Debug().Err(err).Msg("WebSocket heartbeat failed, falling back to HTTP")
	}

	// HTTP fallback
	heartbeat.SendOnce(ctx, httpClient, agentVersion, resticVersion, startedAt)
}

func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the NerdBackup Agent Windows/system service",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Register as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)
			if !config.Exists() {
				return fmt.Errorf("not initialized — run 'nerdbackup-agent init' first")
			}
			binaryPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine binary path: %w", err)
			}
			return service.Install(binaryPath)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)
			return service.Uninstall()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)
			return service.Start()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)
			return service.Stop()
		},
	})

	return cmd
}

func backupCmd() *cobra.Command {
	var repoID string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Trigger an immediate backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("not initialized: %w", err)
			}

			logging.Init(true, false)
			ctx := context.Background()
			client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)

			repos, _, err := client.GetRepos(ctx)
			if err != nil {
				return fmt.Errorf("get repos: %w", err)
			}

			if len(repos) == 0 {
				return fmt.Errorf("no repos configured — add one via the dashboard")
			}

			var target *api.RepoConfig
			for _, r := range repos {
				if repoID == "" || r.ID == repoID {
					target = &r
					break
				}
			}
			if target == nil {
				return fmt.Errorf("repo %s not found", repoID)
			}

			resticBinary, err := restic.FindOrInstall()
			if err != nil {
				return fmt.Errorf("restic not available: %w", err)
			}

			storageEnv := buildStorageEnv(target.StorageConfig)
			runner := restic.NewRunner(resticBinary, target.ResticRepoPath, target.ResticPassword, storageEnv)

			// Remove stale locks
			if err := runner.UnlockIfStale(ctx, 30*time.Minute); err != nil {
				logging.Log.Warn().Err(err).Msg("Stale lock removal failed")
			}

			hostname, _ := os.Hostname()
			tags := append(target.Tags,
				"nerdbackup:agent_id="+cfg.AgentID,
				"nerdbackup:repo_id="+target.ID,
				"nerdbackup:hostname="+hostname,
				"nerdbackup:trigger=manual",
			)

			logging.Log.Info().Strs("paths", target.Paths).Msg("Starting backup")
			startedAt := time.Now()

			summary, err := runner.Backup(ctx, restic.BackupOptions{
				Paths:             target.Paths,
				Excludes:          target.ExcludePatterns,
				Tags:              tags,
				BandwidthLimitKiB: target.BandwidthLimitKiB,
			}, func(p restic.ProgressEntry) {
				fmt.Printf("\r  %.1f%% done, %d files", p.PercentDone*100, p.FilesDone)
			})
			fmt.Println()

			completedAt := time.Now()

			// Report to API
			report := api.JobReportRequest{
				RepoID:      target.ID,
				PolicyID:    target.PolicyID,
				Operation:   "backup",
				StartedAt:   startedAt,
				CompletedAt: completedAt,
			}

			if err != nil {
				report.Status = "failed"
				report.ErrorMessage = err.Error()
				_ = client.ReportJob(ctx, report)
				return fmt.Errorf("backup failed: %w", err)
			}

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
			_ = client.ReportJob(ctx, report)

			logging.Log.Info().
				Str("snapshot", summary.SnapshotID).
				Int("files_new", summary.FilesNew).
				Int64("data_added", summary.DataAdded).
				Msg("Backup completed")

			return nil
		},
	}

	cmd.Flags().StringVar(&repoID, "repo", "", "Repo ID (defaults to first repo)")
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Println("Agent not initialized. Run 'nerdbackup-agent init' first.")
				return nil
			}

			fmt.Printf("Agent ID:      %s\n", cfg.AgentID)
			fmt.Printf("Name:          %s\n", cfg.Name)
			fmt.Printf("API URL:       %s\n", cfg.APIBaseURL)
			fmt.Printf("Config:        %s\n", config.ConfigPath())
			fmt.Printf("Last backup:   %s\n", orDefault(cfg.LastBackupAt, "never"))
			fmt.Printf("Repos init'd:  %d\n", len(cfg.InitializedRepos))

			if resticPath, err := restic.FindOrInstall(); err == nil {
				fmt.Printf("Restic:        %s\n", resticPath)
			} else {
				fmt.Println("Restic:        not found")
			}

			return nil
		},
	}
}

func snapshotsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshots",
		Short: "List restic snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("not initialized: %w", err)
			}

			logging.Init(false, false)
			ctx := context.Background()
			client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)

			repos, _, err := client.GetRepos(ctx)
			if err != nil {
				return fmt.Errorf("get repos: %w", err)
			}

			resticBinary, err := restic.FindOrInstall()
			if err != nil {
				return fmt.Errorf("restic not available: %w", err)
			}

			for _, repo := range repos {
				storageEnv := buildStorageEnv(repo.StorageConfig)
				runner := restic.NewRunner(resticBinary, repo.ResticRepoPath, repo.ResticPassword, storageEnv)

				snapshots, err := runner.Snapshots(ctx)
				if err != nil {
					logging.Log.Error().Err(err).Str("repo", repo.ID).Msg("Failed to list snapshots")
					continue
				}

				fmt.Printf("\nRepo: %s (%s)\n", repo.ID, repo.ResticRepoPath)
				fmt.Printf("%-12s %-20s %-10s %s\n", "ID", "Time", "Host", "Paths")
				for _, s := range snapshots {
					fmt.Printf("%-12s %-20s %-10s %v\n", s.ShortID, s.Time.Format("2006-01-02 15:04"), s.Hostname, s.Paths)
				}
			}

			return nil
		},
	}
}

func restoreCmd() *cobra.Command {
	var repoID string

	cmd := &cobra.Command{
		Use:   "restore SNAPSHOT_ID TARGET_PATH",
		Short: "Restore a snapshot to a target directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID := args[0]
			targetPath := args[1]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("not initialized: %w", err)
			}

			logging.Init(true, false)
			ctx := context.Background()
			client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)

			repos, _, err := client.GetRepos(ctx)
			if err != nil {
				return fmt.Errorf("get repos: %w", err)
			}

			var target *api.RepoConfig
			for _, r := range repos {
				if repoID == "" || r.ID == repoID {
					target = &r
					break
				}
			}
			if target == nil {
				return fmt.Errorf("repo not found")
			}

			resticBinary, err := restic.FindOrInstall()
			if err != nil {
				return fmt.Errorf("restic not available: %w", err)
			}

			storageEnv := buildStorageEnv(target.StorageConfig)
			runner := restic.NewRunner(resticBinary, target.ResticRepoPath, target.ResticPassword, storageEnv)

			logging.Log.Info().Str("snapshot", snapshotID).Str("target", targetPath).Msg("Starting restore")
			startedAt := time.Now()

			err = runner.Restore(ctx, snapshotID, targetPath, nil, nil)
			completedAt := time.Now()

			report := api.JobReportRequest{
				RepoID:           target.ID,
				PolicyID:         target.PolicyID,
				Operation:        "restore",
				StartedAt:        startedAt,
				CompletedAt:      completedAt,
				ResticSnapshotID: snapshotID,
			}

			if err != nil {
				report.Status = "failed"
				report.ErrorMessage = err.Error()
				_ = client.ReportJob(ctx, report)
				return fmt.Errorf("restore failed: %w", err)
			}

			report.Status = "completed"
			_ = client.ReportJob(ctx, report)

			logging.Log.Info().Str("snapshot", snapshotID).Str("target", targetPath).Msg("Restore completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&repoID, "repo", "", "Repo ID (defaults to first repo)")
	return cmd
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks on the agent setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(false, false)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			results := doctor.RunAll(ctx)

			passed := 0
			warned := 0
			failed := 0

			for _, r := range results {
				var icon string
				switch r.Status {
				case "OK":
					icon = "\033[32m✓\033[0m"
					passed++
				case "WARN":
					icon = "\033[33m!\033[0m"
					warned++
				case "FAIL":
					icon = "\033[31m✗\033[0m"
					failed++
				}
				detail := ""
				if r.Detail != "" {
					detail = " — " + r.Detail
				}
				fmt.Printf(" %s %-45s %s%s\n", icon, r.Name, r.Status, detail)
			}

			fmt.Printf("\n%d/%d checks passed", passed, len(results))
			if warned > 0 {
				fmt.Printf(", %d warnings", warned)
			}
			if failed > 0 {
				fmt.Printf(", %d failed", failed)
			}
			fmt.Println()

			if failed > 0 {
				return fmt.Errorf("%d checks failed", failed)
			}
			return nil
		},
	}
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for and install agent updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)

			fmt.Printf("Current version: v%s\n", version)
			fmt.Println("Checking for updates...")

			u := updater.New(version)
			// Force check by using a fresh updater (no rate limit on manual command)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			if version == "dev" {
				fmt.Println("Running development build — skipping update check")
				return nil
			}

			if u.ForceCheckAndUpdate(ctx) {
				fmt.Println("✓ Update installed! Restarting service...")
				if err := service.Restart(); err != nil {
					fmt.Printf("Could not restart service automatically: %v\n", err)
					fmt.Println("Please restart manually:")
					if runtime.GOOS == "windows" {
						fmt.Println("  Restart-Service NerdBackupAgent")
					} else {
						fmt.Println("  systemctl --user restart nerdbackup-agent")
					}
				} else {
					fmt.Println("✓ Service restarted!")
				}
				return nil
			}

			fmt.Printf("Already up to date (v%s)\n", version)
			return nil
		},
	}
}

func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Deregister from NerdBackup, stop service, and clean up",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)

			cfg, err := config.Load()
			if err != nil {
				logging.Log.Warn().Msg("No config found — skipping API deregistration")
			} else {
				// Deregister from the NerdBackup API
				client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				logging.Log.Info().Str("agent_id", cfg.AgentID).Msg("Deregistering agent from NerdBackup")
				if err := client.Deregister(ctx); err != nil {
					logging.Log.Warn().Err(err).Msg("Failed to deregister from API — agent may still appear in dashboard")
				} else {
					logging.Log.Info().Msg("Agent deregistered from NerdBackup")
				}
			}

			// Stop and remove service
			logging.Log.Info().Msg("Stopping service")
			_ = service.Stop()
			logging.Log.Info().Msg("Removing service")
			_ = service.Uninstall()

			// Remove config files
			userConfig := config.UserConfigPath()
			sysConfig := config.SystemConfigPath()
			if err := os.Remove(userConfig); err == nil {
				logging.Log.Info().Str("path", userConfig).Msg("Removed user config")
			}
			if err := os.Remove(sysConfig); err == nil {
				logging.Log.Info().Str("path", sysConfig).Msg("Removed system config")
			}

			fmt.Println("NerdBackup Agent uninstalled.")
			return nil
		},
	}
}

func installServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-service",
		Short: "Install the agent as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !config.Exists() {
				return fmt.Errorf("not initialized — run 'nerdbackup-agent init' first")
			}

			binaryPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine binary path: %w", err)
			}

			return service.Install(binaryPath)
		},
	}
}

func dockerDiscoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docker-discover",
		Short: "Discover Docker volumes and Compose projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(true, false)
			ctx := context.Background()

			if !docker.IsDockerAvailable(ctx) {
				return fmt.Errorf("Docker is not available — ensure the Docker daemon is running and accessible")
			}

			// Discover volumes
			volumes, err := docker.DiscoverVolumes(ctx)
			if err != nil {
				return fmt.Errorf("discover volumes: %w", err)
			}

			fmt.Printf("\nDocker Volumes (%d found):\n", len(volumes))
			fmt.Printf("%-30s %-10s %-50s %s\n", "NAME", "DRIVER", "MOUNTPOINT", "CONTAINERS")
			for _, v := range volumes {
				containers := "none"
				if len(v.Containers) > 0 {
					containers = strings.Join(v.Containers, ", ")
				}
				name := v.Name
				if len(name) > 29 {
					name = name[:26] + "..."
				}
				mp := v.Mountpoint
				if len(mp) > 49 {
					mp = mp[:46] + "..."
				}
				fmt.Printf("%-30s %-10s %-50s %s\n", name, v.Driver, mp, containers)
			}

			// Discover compose projects
			projects, err := docker.DiscoverComposeProjects(ctx)
			if err == nil && len(projects) > 0 {
				fmt.Printf("\nDocker Compose Projects (%d found):\n", len(projects))
				fmt.Printf("%-25s %-15s %s\n", "NAME", "STATUS", "CONFIG")
				for _, p := range projects {
					fmt.Printf("%-25s %-15s %s\n", p.Name, p.Status, p.ConfigFile)
				}
			}

			// Upload to API if agent is configured
			cfg, cfgErr := config.Load()
			if cfgErr == nil {
				client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)
				uploadData := map[string]interface{}{
					"volumes":          volumes,
					"compose_projects": projects,
				}
				uploadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if err := client.PostDockerVolumes(uploadCtx, uploadData); err != nil {
					fmt.Printf("\nNote: Failed to upload to API (%s). Volumes printed above.\n", err)
				} else {
					fmt.Println("\nVolumes uploaded to NerdBackup API.")
				}
			}

			return nil
		},
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

func getResticVersion(binary string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	// Parse "restic 0.17.3 compiled with go1.21.0 on linux/amd64" → "0.17.3"
	line := strings.TrimSpace(string(out))
	parts := strings.Fields(line)
	if len(parts) >= 2 && parts[0] == "restic" {
		return parts[1]
	}
	return line
}

func checkForUpdates(cfg *config.AgentConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := api.NewClient(cfg.APIBaseURL, cfg.AgentID, cfg.AgentToken)
	info, err := client.GetLatestVersion(ctx)
	if err != nil {
		return
	}

	if info.Version != version && version != "dev" {
		logging.Log.Warn().
			Str("current", version).
			Str("latest", info.Version).
			Msg("Agent update available — run 'nerdbackup-agent update' to upgrade")
	}
}

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// hasDisplay checks if a desktop environment is available.
func hasDisplay() bool {
	switch runtime.GOOS {
	case "windows":
		return true // Windows always has a desktop (even Server has a tray)
	case "darwin":
		return true // macOS always has a desktop
	default:
		// Linux: check for DISPLAY or WAYLAND_DISPLAY env var
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	}
}

func trayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tray",
		Short: "Launch the system tray application",
		Long:  "Starts a system tray icon for monitoring and controlling the NerdBackup agent. Requires a desktop environment.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tray.Run(version)
			return nil
		},
	}
}
