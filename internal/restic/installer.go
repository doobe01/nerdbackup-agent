package restic

import (
	"archive/zip"
	"compress/bzip2"
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
	binaryName := "restic"
	if runtime.GOOS == "windows" {
		binaryName = "restic.exe"
	}

	// Check PATH first
	if path, err := exec.LookPath(binaryName); err == nil {
		ver, _ := getVersion(path)
		logging.Log.Info().Str("path", path).Str("version", ver).Msg("Found restic in PATH")
		return path, nil
	}

	// Install to ~/.local/bin
	home, _ := os.UserHomeDir()
	installDir := filepath.Join(home, ".local", "bin")
	installPath := filepath.Join(installDir, binaryName)

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
	if err := downloadAndExtract(installPath, url); err != nil {
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

func downloadAndExtract(dest string, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if strings.HasSuffix(url, ".zip") {
		return extractZip(dest, resp.Body)
	}
	return extractBz2(dest, resp.Body)
}

func extractBz2(dest string, r io.Reader) error {
	bzReader := bzip2.NewReader(r)
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, bzReader)
	return err
}

func extractZip(dest string, r io.Reader) error {
	// Download to temp file first (zip needs random access)
	tmp, err := os.CreateTemp("", "restic-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if _, err := io.Copy(tmp, r); err != nil {
		return err
	}

	stat, err := tmp.Stat()
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(tmp, stat.Size())
	if err != nil {
		return err
	}

	for _, f := range zr.File {
		if strings.Contains(f.Name, "restic") && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.Create(dest)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, rc)
			return err
		}
	}

	return fmt.Errorf("restic binary not found in zip archive")
}

func getVersion(binary string) (string, error) {
	out, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
