package restic

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

const resticVersion = "0.17.3"

// FindOrInstall locates restic in PATH, or downloads it.
func FindOrInstall() (string, error) {
	// Check PATH first
	if path, err := exec.LookPath("restic"); err == nil {
		ver, _ := getVersion(path)
		logging.Log.Info().Str("path", path).Str("version", ver).Msg("Found restic in PATH")
		return path, nil
	}

	// Install to ~/.local/bin
	home, _ := os.UserHomeDir()
	installDir := filepath.Join(home, ".local", "bin")
	installPath := filepath.Join(installDir, "restic")

	if _, err := os.Stat(installPath); err == nil {
		ver, _ := getVersion(installPath)
		logging.Log.Info().Str("path", installPath).Str("version", ver).Msg("Found installed restic")
		return installPath, nil
	}

	logging.Log.Info().Str("version", resticVersion).Msg("Downloading restic")

	if err := os.MkdirAll(installDir, 0755); err != nil {
		return "", err
	}

	url := downloadURL()
	if err := downloadFile(installPath, url); err != nil {
		return "", fmt.Errorf("download restic: %w", err)
	}

	if err := os.Chmod(installPath, 0755); err != nil {
		return "", err
	}

	ver, _ := getVersion(installPath)
	logging.Log.Info().Str("path", installPath).Str("version", ver).Msg("Restic installed")
	return installPath, nil
}

func downloadURL() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ext := "bz2"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf(
		"https://github.com/restic/restic/releases/download/v%s/restic_%s_%s_%s.%s",
		resticVersion, resticVersion, goos, goarch, ext,
	)
}

func downloadFile(dest string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func getVersion(binary string) (string, error) {
	out, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
