package updater

import (
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

const (
	githubRepo     = "doobe01/nerdbackup-agent"
	checkInterval  = 1 * time.Hour
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// AutoUpdater silently checks for and installs agent updates.
type AutoUpdater struct {
	currentVersion string
	lastCheck      time.Time
}

// New creates a new AutoUpdater.
func New(currentVersion string) *AutoUpdater {
	return &AutoUpdater{currentVersion: currentVersion}
}

// CheckAndUpdate checks for a new version and installs it if available.
// Safe to call frequently — internally rate-limited to once per hour.
// Returns true if an update was installed (caller should restart).
func (u *AutoUpdater) CheckAndUpdate(ctx context.Context) bool {
	return u.checkAndUpdate(ctx, false)
}

// ForceCheckAndUpdate bypasses the rate limit. Use for manual `update` command.
func (u *AutoUpdater) ForceCheckAndUpdate(ctx context.Context) bool {
	return u.checkAndUpdate(ctx, true)
}

func (u *AutoUpdater) checkAndUpdate(ctx context.Context, force bool) bool {
	if u.currentVersion == "dev" {
		return false
	}

	if !force && time.Since(u.lastCheck) < checkInterval {
		return false
	}
	u.lastCheck = time.Now()

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		logging.Log.Debug().Err(err).Msg("Auto-update: failed to check for updates")
		return false
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == u.currentVersion {
		return false
	}

	logging.Log.Info().
		Str("current", u.currentVersion).
		Str("latest", latestVersion).
		Msg("Auto-update: new version available, downloading")

	if err := downloadAndReplace(ctx, release); err != nil {
		logging.Log.Error().Err(err).Msg("Auto-update: failed to install update")
		return false
	}

	logging.Log.Info().Str("version", latestVersion).Msg("Auto-update: installed successfully, restart required")
	return true
}

func fetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NerdBackup-Agent")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func downloadAndReplace(ctx context.Context, release *GitHubRelease) error {
	// Find the right asset for this platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	binaryName := "nerdbackup-agent"
	if goos == "windows" {
		binaryName = "nerdbackup-agent.exe"
	}

	var assetURL string
	expectedName := fmt.Sprintf("nerdbackup-agent_%s_%s", goos, goarch)
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, expectedName) && !strings.Contains(asset.Name, "installer") && !strings.Contains(asset.Name, "setup") {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}

	if assetURL == "" {
		return fmt.Errorf("no asset found for %s/%s", goos, goarch)
	}

	// Download the archive
	req, err := http.NewRequestWithContext(ctx, "GET", assetURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Read entire archive into memory
	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Extract the binary to a temp file
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine self path: %w", err)
	}
	selfDir := filepath.Dir(selfPath)
	tmpPath := filepath.Join(selfDir, binaryName+".update")

	if strings.HasSuffix(assetURL, ".zip") {
		if err := extractFromZip(archiveData, binaryName, tmpPath); err != nil {
			return err
		}
	} else {
		if err := extractFromBz2(archiveData, tmpPath); err != nil {
			return err
		}
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Swap: rename current → .old, rename .update → current
	oldPath := selfPath + ".old"
	os.Remove(oldPath) // clean up any previous .old file

	if err := os.Rename(selfPath, oldPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename current binary: %w", err)
	}

	if err := os.Rename(tmpPath, selfPath); err != nil {
		// Rollback
		_ = os.Rename(oldPath, selfPath)
		return fmt.Errorf("rename new binary: %w", err)
	}

	// Clean up old binary (best effort — on Windows this may fail while running)
	os.Remove(oldPath)

	return nil
}

func extractFromZip(data []byte, targetName, destPath string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	target := strings.TrimSuffix(strings.ToLower(targetName), ".exe")
	for _, f := range r.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if !strings.Contains(name, target) || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		out, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, rc)
		return err
	}
	return fmt.Errorf("%s not found in zip", targetName)
}

func extractFromBz2(data []byte, destPath string) error {
	bzReader := bzip2.NewReader(bytes.NewReader(data))
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, bzReader)
	return err
}
