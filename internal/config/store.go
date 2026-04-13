package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

const configDir = ".nerdbackup"
const configFile = "config.json"

// ConfigPath returns the full path to the config file.
// Checks system-wide path first (for Windows Service), then user home.
func ConfigPath() string {
	// On Windows, check system-wide path first (used by Windows Service)
	if runtime.GOOS == "windows" {
		sysPath := systemConfigPath()
		if sysPath != "" {
			if _, err := os.Stat(sysPath); err == nil {
				return sysPath
			}
		}
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, configDir, configFile)
}

// UserConfigPath returns the user-specific config path (always in home dir).
func UserConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configDir, configFile)
}

// SystemConfigPath returns the system-wide config path.
// On Windows: %PROGRAMDATA%\NerdBackup\config.json
// On Linux/macOS: /etc/nerdbackup/config.json
func SystemConfigPath() string {
	return systemConfigPath()
}

func systemConfigPath() string {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "NerdBackup", configFile)
	}
	return filepath.Join("/etc", "nerdbackup", configFile)
}

// Load reads the config from disk.
func Load() (*AgentConfig, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil, err
	}
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the config to disk atomically (write to .tmp, then rename).
func Save(cfg *AgentConfig) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// CopyToSystemPath copies the user config to the system-wide path.
// This is needed for Windows Services which run as LOCAL SYSTEM.
func CopyToSystemPath() error {
	userPath := UserConfigPath()
	sysPath := systemConfigPath()

	data, err := os.ReadFile(userPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(sysPath), 0700); err != nil {
		return err
	}

	return os.WriteFile(sysPath, data, 0600)
}

// Exists checks if a config file exists.
func Exists() bool {
	_, err := os.Stat(ConfigPath())
	return err == nil
}
