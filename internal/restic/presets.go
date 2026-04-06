package restic

import (
	"embed"
	"strings"
)

//go:embed presets/*.txt presets/*.sh
var presetFS embed.FS

var presetNames = map[string]string{
	"developer":   "presets/developer.txt",
	"macos":       "presets/macos.txt",
	"windows":     "presets/windows.txt",
	"full-system": "presets/full-system.txt",
	"golang":      "presets/golang.txt",
	"rust":        "presets/rust.txt",
	"ruby":        "presets/ruby.txt",
	"docker":      "presets/docker.txt",
	"kubernetes":  "presets/kubernetes.txt",
	"database":    "presets/database.txt",
}

// GetPresetPreHook returns the embedded pre-backup hook script for a preset, if any.
func GetPresetPreHook(name string) string {
	filename := "presets/" + name + "-pre.sh"
	data, err := presetFS.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(data)
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
