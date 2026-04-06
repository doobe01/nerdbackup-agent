package restic

import (
	"testing"
)

func TestGetPresetExcludes(t *testing.T) {
	tests := []struct {
		name     string
		minItems int
		contains string
	}{
		{"developer", 5, "node_modules/"},
		{"macos", 3, ".DS_Store"},
		{"windows", 3, "Thumbs.db"},
		{"full-system", 5, "/proc/*"},
	}

	for _, tt := range tests {
		excludes := GetPresetExcludes(tt.name)
		if len(excludes) < tt.minItems {
			t.Errorf("GetPresetExcludes(%q): got %d items, want at least %d", tt.name, len(excludes), tt.minItems)
		}

		found := false
		for _, e := range excludes {
			if e == tt.contains {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetPresetExcludes(%q): should contain %q", tt.name, tt.contains)
		}
	}
}

func TestGetPresetExcludesUnknown(t *testing.T) {
	excludes := GetPresetExcludes("nonexistent")
	if excludes != nil {
		t.Errorf("Expected nil for unknown preset, got %v", excludes)
	}
}

func TestMergeExcludes(t *testing.T) {
	result := MergeExcludes(
		[]string{"developer", "macos"},
		[]string{"custom-pattern", ".DS_Store"}, // .DS_Store is in macos preset — should dedup
	)

	if len(result) == 0 {
		t.Fatal("MergeExcludes returned empty result")
	}

	// Check deduplication — .DS_Store should appear only once
	count := 0
	for _, r := range result {
		if r == ".DS_Store" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected .DS_Store once, found %d times", count)
	}

	// Check custom pattern is included
	found := false
	for _, r := range result {
		if r == "custom-pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Custom pattern should be in merged result")
	}
}

func TestMergeExcludesEmpty(t *testing.T) {
	result := MergeExcludes(nil, nil)
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d items", len(result))
	}
}

func TestGetPresetPreHook(t *testing.T) {
	hook := GetPresetPreHook("full-system")
	if hook == "" {
		t.Error("full-system preset should have a pre-hook script")
	}
	if len(hook) < 100 {
		t.Error("Pre-hook script seems too short")
	}
}

func TestGetPresetPreHookMissing(t *testing.T) {
	hook := GetPresetPreHook("developer")
	if hook != "" {
		t.Error("developer preset should not have a pre-hook script")
	}
}
