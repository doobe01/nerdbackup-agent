package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	// Use temp directory
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := &AgentConfig{
		AgentID:    "test-agent-123",
		AgentToken: "nb_agent_testtoken",
		APIBaseURL: "https://nerdbackup.com",
		Name:       "test-agent",
		Debug:      true,
		StartedAt:  time.Now().Truncate(time.Second),
	}

	// Save
	err := Save(cfg)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, configDir, configFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Verify file permissions (skip on Windows — no POSIX permissions)
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("Expected permissions 0600, got %o", info.Mode().Perm())
		}
	}

	// Load
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.AgentID != cfg.AgentID {
		t.Errorf("AgentID mismatch: got %q, want %q", loaded.AgentID, cfg.AgentID)
	}
	if loaded.AgentToken != cfg.AgentToken {
		t.Errorf("AgentToken mismatch: got %q, want %q", loaded.AgentToken, cfg.AgentToken)
	}
	if loaded.Name != cfg.Name {
		t.Errorf("Name mismatch: got %q, want %q", loaded.Name, cfg.Name)
	}
	if loaded.Debug != cfg.Debug {
		t.Errorf("Debug mismatch: got %v, want %v", loaded.Debug, cfg.Debug)
	}
}

func TestAtomicSave(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := &AgentConfig{AgentID: "first", Name: "first"}
	Save(cfg)

	// Save again — should atomically replace
	cfg.AgentID = "second"
	cfg.Name = "second"
	Save(cfg)

	loaded, _ := Load()
	if loaded.AgentID != "second" {
		t.Errorf("Expected second save to overwrite: got %q", loaded.AgentID)
	}

	// Verify no .tmp file left behind
	tmpPath := filepath.Join(tmpDir, configDir, configFile+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temp file should not exist after successful save")
	}
}

func TestExists(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	if Exists() {
		t.Error("Exists should return false before save")
	}

	Save(&AgentConfig{AgentID: "test"})

	if !Exists() {
		t.Error("Exists should return true after save")
	}
}

func TestLoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := Load()
	if err == nil {
		t.Error("Load should fail when no config file exists")
	}
}

func TestIsRepoInitialized(t *testing.T) {
	cfg := &AgentConfig{}

	if cfg.IsRepoInitialized("repo-1") {
		t.Error("Should not be initialized initially")
	}

	cfg.MarkRepoInitialized("repo-1")

	if !cfg.IsRepoInitialized("repo-1") {
		t.Error("Should be initialized after marking")
	}

	if cfg.IsRepoInitialized("repo-2") {
		t.Error("Different repo should not be initialized")
	}

	// Marking again should not duplicate
	cfg.MarkRepoInitialized("repo-1")
	if len(cfg.InitializedRepos) != 1 {
		t.Errorf("Expected 1 repo, got %d", len(cfg.InitializedRepos))
	}
}
