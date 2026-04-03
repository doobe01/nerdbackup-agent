package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/doobe01/nerdbackup-agent/internal/logging"
)

var pendingMu sync.Mutex

func pendingPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nerdbackup", "pending_reports.json")
}

// SavePendingReport persists a failed job report for later retry.
func SavePendingReport(report JobReportRequest) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	reports := loadPendingUnsafe()
	reports = append(reports, report)

	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		logging.Log.Error().Err(err).Msg("Failed to marshal pending reports")
		return
	}

	if err := os.WriteFile(pendingPath(), data, 0600); err != nil {
		logging.Log.Error().Err(err).Msg("Failed to save pending reports")
	}
}

// LoadPendingReports reads all pending reports from disk.
func LoadPendingReports() []JobReportRequest {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return loadPendingUnsafe()
}

// ClearPendingReports removes the pending reports file.
func ClearPendingReports() {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	os.Remove(pendingPath())
}

func loadPendingUnsafe() []JobReportRequest {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil
	}
	var reports []JobReportRequest
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil
	}
	return reports
}
