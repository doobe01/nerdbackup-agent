//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

func Install(binaryPath string) error {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".nerdbackup")
	os.MkdirAll(logDir, 0700)

	taskName := "NerdBackupAgent"

	// Delete existing task if present (ignore error — may not exist)
	if err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run(); err != nil {
		logging.Log.Debug().Err(err).Msg("No existing scheduled task to delete")
	}

	// Create task that runs at user login
	// Try with HIGHEST run level first (needs admin), fall back to normal
	err := exec.Command("schtasks", "/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" run`, binaryPath),
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F",
	).Run()
	if err != nil {
		logging.Log.Debug().Msg("HIGHEST run level failed, trying without elevation")
		err = exec.Command("schtasks", "/Create",
			"/TN", taskName,
			"/TR", fmt.Sprintf(`"%s" run`, binaryPath),
			"/SC", "ONLOGON",
			"/F",
		).Run()
	}
	if err != nil {
		return fmt.Errorf("create scheduled task: %w", err)
	}

	// Start it now
	if err := exec.Command("schtasks", "/Run", "/TN", taskName).Run(); err != nil {
		logging.Log.Warn().Err(err).Msg("Failed to start scheduled task immediately")
	}

	fmt.Printf("Scheduled task '%s' created.\n", taskName)
	fmt.Println("Agent will start at login and is running now.")
	fmt.Println("  schtasks /Query /TN NerdBackupAgent")
	return nil
}
