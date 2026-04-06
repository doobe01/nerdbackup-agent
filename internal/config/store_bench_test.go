package config

import (
	"os"
	"testing"
)

func BenchmarkSaveAndLoad(b *testing.B) {
	tmpDir := b.TempDir()
	os.Setenv("HOME", tmpDir)
	os.Setenv("USERPROFILE", tmpDir)
	defer func() {
		os.Unsetenv("HOME")
		os.Unsetenv("USERPROFILE")
	}()

	cfg := &AgentConfig{
		AgentID:    "bench-agent",
		AgentToken: "nb_agent_benchtoken",
		APIBaseURL: "https://nerdbackup.com",
		Name:       "bench",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Save(cfg)
		_, _ = Load()
	}
}
