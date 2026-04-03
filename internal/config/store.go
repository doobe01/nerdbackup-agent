package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const configDir = ".nerdbackup"
const configFile = "config.json"

// ConfigPath returns the full path to the config file.
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configDir, configFile)
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

// Exists checks if a config file exists.
func Exists() bool {
	_, err := os.Stat(ConfigPath())
	return err == nil
}
