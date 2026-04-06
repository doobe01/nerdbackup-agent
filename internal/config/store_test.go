package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	return tmpDir, func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir, cleanup := setupTestDir(t)
	defer cleanup()

	cfg := &AgentConfig{
		AgentID:    "test-agent-123",
		AgentToken: "nb_agent_testtoken",
		APIBaseURL: "https://nerdbackup.com",
		Name:       "test-agent",
		Debug:      true,
		StartedAt:  time.Now().Truncate(time.Second),
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	path := filepath.Join(tmpDir, configDir, configFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("Expected permissions 0600, got %o", info.Mode().Perm())
		}
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.AgentID != cfg.AgentID {
		t.Errorf("AgentID: got %q, want %q", loaded.AgentID, cfg.AgentID)
	}
	if loaded.Name != cfg.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, cfg.Name)
	}
}

func TestAtomicSave(t *testing.T) {
	tmpDir, cleanup := setupTestDir(t)
	defer cleanup()

	if err := Save(&AgentConfig{AgentID: "first"}); err != nil {
		t.Fatalf("First save: %v", err)
	}
	if err := Save(&AgentConfig{AgentID: "second"}); err != nil {
		t.Fatalf("Second save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AgentID != "second" {
		t.Errorf("Expected second, got %q", loaded.AgentID)
	}

	tmpPath := filepath.Join(tmpDir, configDir, configFile+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temp file should not exist after save")
	}
}

func TestExists(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	if Exists() {
		t.Error("Exists should return false before save")
	}
	if err := Save(&AgentConfig{AgentID: "test"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists() {
		t.Error("Exists should return true after save")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

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
	cfg.MarkRepoInitialized("repo-1")
	if len(cfg.InitializedRepos) != 1 {
		t.Errorf("Expected 1 repo, got %d", len(cfg.InitializedRepos))
	}
}
