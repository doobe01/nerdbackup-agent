//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Install(binaryPath string) error {
	// On Windows, create a scheduled task that runs at login
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".nerdbackup")
	os.MkdirAll(logDir, 0700)

	taskName := "NerdBackupAgent"

	// Delete existing task if present
	exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()

	// Create task that runs at user login
	err := exec.Command("schtasks", "/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" run`, binaryPath),
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F",
	).Run()
	if err != nil {
		return fmt.Errorf("create scheduled task: %w", err)
	}

	// Start it now
	exec.Command("schtasks", "/Run", "/TN", taskName).Run()

	fmt.Printf("Scheduled task '%s' created.\n", taskName)
	fmt.Println("Agent will start at login and is running now.")
	fmt.Println("  schtasks /Query /TN NerdBackupAgent")
	return nil
}
