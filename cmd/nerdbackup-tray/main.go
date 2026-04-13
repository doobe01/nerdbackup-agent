package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"

	"github.com/doobe01/nerdbackup-agent/internal/config"
	"github.com/doobe01/nerdbackup-agent/internal/localapi"
	"github.com/doobe01/nerdbackup-agent/internal/service"
)

const (
	apiBase      = "http://127.0.0.1:19284"
	pollInterval = 5 * time.Second
	httpTimeout  = 3 * time.Second
)

var (
	version = "dev"

	// Menu items (set in onReady).
	mStatus     *systray.MenuItem
	mLastBackup *systray.MenuItem
	mBackupNow  *systray.MenuItem
	mDashboard  *systray.MenuItem
	mLogs       *systray.MenuItem
	mStart      *systray.MenuItem
	mStop       *systray.MenuItem
	mRestart    *systray.MenuItem
	mUpdate     *systray.MenuItem
	mAbout      *systray.MenuItem
	mQuit       *systray.MenuItem

	lastStatus    *localapi.AgentStatus
	lastWasOnline bool

	httpClient = &http.Client{Timeout: httpTimeout}
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconOffline)
	systray.SetTitle("NerdBackup")
	systray.SetTooltip("NerdBackup Agent — Connecting...")

	// Title (disabled — informational only).
	mTitle := systray.AddMenuItem("NerdBackup Agent", "")
	mTitle.Disable()

	systray.AddSeparator()

	// Status section.
	mStatus = systray.AddMenuItem("● Connecting...", "Agent status")
	mStatus.Disable()
	mLastBackup = systray.AddMenuItem("Last backup: —", "")
	mLastBackup.Disable()

	systray.AddSeparator()

	// Actions.
	mBackupNow = systray.AddMenuItem("Back Up Now", "Trigger a backup")
	mDashboard = systray.AddMenuItem("Open Dashboard", "Open NerdBackup dashboard in browser")
	mLogs = systray.AddMenuItem("View Logs", "Open agent log file")

	systray.AddSeparator()

	// Service control.
	mStart = systray.AddMenuItem("Start Service", "Start the agent service")
	mStop = systray.AddMenuItem("Stop Service", "Stop the agent service")
	mRestart = systray.AddMenuItem("Restart Service", "Restart the agent service")

	systray.AddSeparator()

	// Update and about.
	mUpdate = systray.AddMenuItem("Check for Updates", "")
	mAbout = systray.AddMenuItem(fmt.Sprintf("About (v%s)", version), "")
	mAbout.Disable()

	systray.AddSeparator()

	mQuit = systray.AddMenuItem("Quit", "Quit the tray app")

	// Start polling the local API.
	go pollLoop()

	// Handle menu clicks.
	go handleClicks()
}

func onExit() {
	// Cleanup — nothing to do for now.
}

// handleClicks dispatches menu item click events.
func handleClicks() {
	for {
		select {
		case <-mBackupNow.ClickedCh:
			go triggerBackup()
		case <-mDashboard.ClickedCh:
			go openDashboard()
		case <-mLogs.ClickedCh:
			go openLogs()
		case <-mStart.ClickedCh:
			go controlService(service.Start, "started")
		case <-mStop.ClickedCh:
			go controlService(service.Stop, "stopped")
		case <-mRestart.ClickedCh:
			go controlService(service.Restart, "restarted")
		case <-mUpdate.ClickedCh:
			go checkUpdate()
		case <-mQuit.ClickedCh:
			systray.Quit()
		}
	}
}

// ---------------------------------------------------------------------------
// Polling
// ---------------------------------------------------------------------------

func pollLoop() {
	for {
		status := fetchStatus()
		if status != nil {
			updateUI(status)

			// Notify on status transitions.
			if lastStatus != nil {
				if lastWasOnline && !status.Online {
					_ = beeep.Notify("NerdBackup", "Agent went offline", "")
				}
				if !lastWasOnline && status.Online {
					_ = beeep.Notify("NerdBackup", "Agent is online", "")
				}
			}

			lastWasOnline = status.Online
			lastStatus = status
		} else {
			// Agent local API not reachable.
			systray.SetIcon(iconOffline)
			mStatus.SetTitle("● Agent not running")
			systray.SetTooltip("NerdBackup Agent — Not running")
			lastWasOnline = false
		}

		// Check if a backup is currently running.
		progress := fetchProgress()
		if progress != nil && progress.Running {
			systray.SetIcon(iconBusy)
			pct := progress.Progress.PercentDone * 100
			mStatus.SetTitle(fmt.Sprintf("● Backing up — %.0f%%", pct))
			systray.SetTooltip(fmt.Sprintf("NerdBackup Agent — Backup %.0f%%", pct))
		}

		time.Sleep(pollInterval)
	}
}

func fetchStatus() *localapi.AgentStatus {
	resp, err := httpClient.Get(apiBase + "/status")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var status localapi.AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil
	}
	return &status
}

// progressResponse mirrors the JSON returned by GET /progress.
type progressResponse struct {
	Running  bool                    `json:"running"`
	Progress *localapi.BackupProgress `json:"progress,omitempty"`
}

func fetchProgress() *progressResponse {
	resp, err := httpClient.Get(apiBase + "/progress")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var p progressResponse
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil
	}
	return &p
}

func updateUI(status *localapi.AgentStatus) {
	if status.Online {
		systray.SetIcon(iconOnline)
		mStatus.SetTitle("● Online — " + status.Uptime)
		systray.SetTooltip("NerdBackup Agent — Online")
	} else {
		systray.SetIcon(iconOffline)
		mStatus.SetTitle("● Offline")
		systray.SetTooltip("NerdBackup Agent — Offline")
	}

	if status.LastBackupAt != "" {
		mLastBackup.SetTitle("Last backup: " + formatRelative(status.LastBackupAt))
	} else {
		mLastBackup.SetTitle("Last backup: never")
	}
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func triggerBackup() {
	// Fetch repo list and trigger the first one.
	resp, err := httpClient.Get(apiBase + "/repos")
	if err != nil {
		_ = beeep.Notify("NerdBackup", "Cannot connect to agent", "")
		return
	}
	defer resp.Body.Close()

	var repos []localapi.RepoStatus
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil || len(repos) == 0 {
		_ = beeep.Notify("NerdBackup", "No repos configured", "")
		return
	}

	// Trigger the first repo.
	triggerResp, err := httpClient.Post(apiBase+"/backup/"+repos[0].ID, "application/json", nil)
	if err != nil {
		_ = beeep.Notify("NerdBackup", "Failed to trigger backup", "")
		return
	}
	defer triggerResp.Body.Close()

	_ = beeep.Notify("NerdBackup", "Backup started for "+strings.Join(repos[0].Paths, ", "), "")
}

func openDashboard() {
	url := "https://nerdbackup.com/dashboard"
	if lastStatus != nil && lastStatus.APIURL != "" {
		url = lastStatus.APIURL + "/dashboard"
	}
	openBrowser(url)
}

func openLogs() {
	// Try the local API first — it returns the actual log path.
	resp, err := httpClient.Get(apiBase + "/logs?lines=1")
	if err == nil {
		defer resp.Body.Close()
		var result struct {
			Path string `json:"path"`
		}
		if json.NewDecoder(resp.Body).Decode(&result) == nil && result.Path != "" {
			openFileInEditor(result.Path)
			return
		}
	}

	// Fallback: derive log path from known defaults.
	home, _ := os.UserHomeDir()
	logPath := home + "/.nerdbackup/agent.log"
	openFileInEditor(logPath)
}

func openFileInEditor(path string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("notepad", path).Start()
	case "darwin":
		_ = exec.Command("open", "-a", "Console", path).Start()
	default:
		// Linux: try xdg-open first, then fall back to xterm+less.
		if err := exec.Command("xdg-open", path).Start(); err != nil {
			_ = exec.Command("xterm", "-e", "less", path).Start()
		}
	}
}

func controlService(fn func() error, verb string) {
	if err := fn(); err != nil {
		_ = beeep.Notify("NerdBackup", fmt.Sprintf("Failed to %s service: %v", verb, err), "")
		return
	}
	_ = beeep.Notify("NerdBackup", fmt.Sprintf("Service %s", verb), "")
}

func checkUpdate() {
	cfg, err := config.Load()
	if err != nil {
		_ = beeep.Notify("NerdBackup", "Cannot load config — is the agent initialized?", "")
		return
	}

	msg := fmt.Sprintf("Current version: v%s\nAgent: %s\nRun 'nerdbackup-agent update' to check for updates.", version, cfg.AgentID)
	_ = beeep.Notify("NerdBackup", msg, "")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func formatRelative(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
