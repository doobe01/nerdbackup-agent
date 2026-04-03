package api

import "time"

// ApiResponse is the standard NerdBackup API envelope.
type ApiResponse[T any] struct {
	Data T    `json:"data"`
	Meta Meta `json:"meta"`
}

type Meta struct {
	RequestID string `json:"request_id"`
	Cursor    string `json:"cursor,omitempty"`
}

// Agent registration
type RegisterAgentRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
}

type RegisterAgentResponse struct {
	ID    string `json:"id"`
	Token string `json:"token"` // shown once
}

// Heartbeat
type HeartbeatRequest struct {
	AgentVersion   string `json:"agent_version"`
	ResticVersion  string `json:"restic_version"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	Hostname       string `json:"hostname"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	LastBackupAt   string `json:"last_backup_at,omitempty"`
	DiskFreeBytes  int64  `json:"disk_free_bytes"`
	CPUCount       int    `json:"cpu_count"`
	MemTotalBytes  int64  `json:"memory_total_bytes"`
}

// Job report
type JobReportRequest struct {
	RepoID          string    `json:"repo_id"`
	PolicyID        string    `json:"policy_id,omitempty"`
	Operation       string    `json:"operation"` // "backup" or "restore"
	Status          string    `json:"status"`    // "completed" or "failed"
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at"`
	ResticSnapshotID string   `json:"restic_snapshot_id,omitempty"`
	Stats           JobStats  `json:"stats"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

type JobStats struct {
	FilesNew            int   `json:"files_new"`
	FilesChanged        int   `json:"files_changed"`
	FilesUnmodified     int   `json:"files_unmodified"`
	DirsNew             int   `json:"dirs_new"`
	DataAddedBytes      int64 `json:"data_added_bytes"`
	TotalFilesProcessed int   `json:"total_files_processed"`
	TotalBytesProcessed int64 `json:"total_bytes_processed"`
	TotalDurationSec    int   `json:"total_duration_seconds"`
}

// Repo config (returned by GET /agents/:id/repos)
type RepoConfig struct {
	ID                    string   `json:"id"`
	StorageBackendID      string   `json:"storage_backend_id"`
	PolicyID              string   `json:"policy_id,omitempty"`
	ResticRepoPath        string   `json:"restic_repo_path"`
	ResticPasswordEncrypted string `json:"restic_password_encrypted"`
	Paths                 []string `json:"paths"`
	ExcludePatterns       []string `json:"exclude_patterns"`
	Tags                  []string `json:"tags"`
	ScheduleCron          string   `json:"schedule_cron,omitempty"`
	StorageConfig         StorageBackendConfig `json:"storage_config"`
}

type StorageBackendConfig struct {
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	AccessKeyID    string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}
