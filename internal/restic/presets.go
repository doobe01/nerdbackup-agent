package restic

import (
	"embed"
	"strings"
)

//go:embed presets/*.txt
var presetFS embed.FS

var presetNames = map[string]string{
	"developer": "presets/developer.txt",
	"macos":     "presets/macos.txt",
	"windows":   "presets/windows.txt",
}

// GetPresetExcludes returns the exclude patterns for a named preset.
func GetPresetExcludes(name string) []string {
	filename, ok := presetNames[name]
	if !ok {
		return nil
	}
	data, err := presetFS.ReadFile(filename)
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// MergeExcludes combines preset excludes with custom exclude patterns.
func MergeExcludes(presets []string, custom []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, preset := range presets {
		for _, p := range GetPresetExcludes(preset) {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}
	for _, p := range custom {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}
