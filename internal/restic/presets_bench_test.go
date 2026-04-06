package restic

import "testing"

func BenchmarkGetPresetExcludes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetPresetExcludes("developer")
	}
}

func BenchmarkGetPresetExcludesAllPresets(b *testing.B) {
	presets := []string{"developer", "macos", "windows", "full-system", "golang", "rust", "ruby", "docker", "kubernetes", "database"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range presets {
			GetPresetExcludes(p)
		}
	}
}

func BenchmarkMergeExcludes(b *testing.B) {
	presets := []string{"developer", "macos", "docker"}
	custom := []string{"*.log", "*.tmp", "secrets/", ".env.local", "node_modules/.cache/"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MergeExcludes(presets, custom)
	}
}
