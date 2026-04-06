package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	configMarker = "NERDBACKUP_CONFIG:"
	githubRepo   = "doobe01/nerdbackup-agent"
)

type InstallConfig struct {
	Token  string `json:"token"`
	APIURL string `json:"api_url"`
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func main() {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════╗")
	fmt.Println("  ║       NerdBackup Agent Installer     ║")
	fmt.Println("  ╚══════════════════════════════════════╝")
	fmt.Println()

	// Read appended config from our own binary
	config, err := readAppendedConfig()
	if err != nil {
		fail("No install configuration found. Download a fresh installer from your NerdBackup dashboard.")
		return
	}

	info("Configuration loaded")

	installDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "NerdBackup")
	if installDir == filepath.Join("", "NerdBackup") {
		// Fallback if LOCALAPPDATA not set
		home, _ := os.UserHomeDir()
		installDir = filepath.Join(home, ".nerdbackup", "bin")
	}

	if err := os.MkdirAll(installDir, 0755); err != nil {
		fail("Failed to create install directory: " + err.Error())
		return
	}

	// 1. Get latest release version
	info("Fetching latest version...")
	version, err := getLatestVersion()
	if err != nil {
		fail("Could not determine latest version: " + err.Error())
		return
	}
	info(fmt.Sprintf("Latest version: %s", version))

	// 2. Download agent binary
	agentExe := filepath.Join(installDir, "nerdbackup-agent.exe")
	arch := runtime.GOARCH
	zipURL := fmt.Sprintf("https://github.com/%s/releases/download/v%s/nerdbackup-agent_windows_%s.zip", githubRepo, version, arch)

	info(fmt.Sprintf("Downloading agent from %s...", zipURL))
	if err := downloadAndExtract(zipURL, installDir, "nerdbackup-agent.exe"); err != nil {
		fail("Download failed: " + err.Error())
		return
	}
	info(fmt.Sprintf("Agent installed to %s", agentExe))

	// 3. Add to PATH
	addToPath(installDir)

	// 4. Download restic
	info("Downloading restic...")
	resticURL := fmt.Sprintf("https://github.com/restic/restic/releases/download/v0.17.3/restic_0.17.3_windows_%s.zip", arch)
	if err := downloadAndExtract(resticURL, installDir, "restic.exe"); err != nil {
		warn("Failed to download restic: " + err.Error())
		warn("You can install it manually later: winget install restic.restic")
	} else {
		info("Restic installed")
	}

	// 5. Register agent with install token
	info("Registering agent...")
	cmd := exec.Command(agentExe, "init", "--install-token", config.Token, "--api-url", config.APIURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail("Agent registration failed: " + err.Error())
		return
	}

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════╗")
	fmt.Println("  ║   NerdBackup Agent is running!       ║")
	fmt.Println("  ║                                      ║")
	fmt.Printf("  ║   Dashboard: %-24s║\n", config.APIURL+"/dashboard")
	fmt.Println("  ╚══════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  Press Enter to close...")
	fmt.Scanln()
}

func readAppendedConfig() (*InstallConfig, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot find executable path: %w", err)
	}

	data, err := os.ReadFile(exePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read executable: %w", err)
	}

	marker := []byte(configMarker)
	idx := bytes.LastIndex(data, marker)
	if idx == -1 {
		return nil, fmt.Errorf("no config marker found")
	}

	jsonData := data[idx+len(marker):]
	// Trim any trailing nulls or whitespace
	jsonData = bytes.TrimRight(jsonData, "\x00 \n\r\t")

	var config InstallConfig
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}

	if config.Token == "" {
		return nil, fmt.Errorf("empty token in config")
	}

	return &config, nil
}

func getLatestVersion() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

func downloadAndExtract(url, destDir, targetFile string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read entire zip into memory (typically <15MB)
	zipData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	target := strings.TrimSuffix(strings.ToLower(targetFile), ".exe")
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if !strings.Contains(name, target) || f.FileInfo().IsDir() {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outPath := filepath.Join(destDir, targetFile)
		out, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return err
		}

		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		return copyErr
	}

	return fmt.Errorf("%s not found in zip", targetFile)
}

func addToPath(dir string) {
	// Check if already in PATH
	path := os.Getenv("PATH")
	if strings.Contains(strings.ToLower(path), strings.ToLower(dir)) {
		return
	}

	info("Adding to PATH...")
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`[Environment]::SetEnvironmentVariable('PATH', $env:PATH + ';%s', 'User')`, dir))
	_ = cmd.Run()
}

func info(msg string) {
	fmt.Printf("  [+] %s\n", msg)
}

func warn(msg string) {
	fmt.Printf("  [!] %s\n", msg)
}

func fail(msg string) {
	fmt.Printf("  [ERROR] %s\n", msg)
	fmt.Println()
	fmt.Println("  Press Enter to close...")
	fmt.Scanln()
	os.Exit(1)
}
