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
	Token string `json:"token"`
}

type RegisterWithTokenResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
	APIBaseURL string `json:"api_base_url"`
}

// Pending backup trigger from dashboard
type PendingBackup struct {
	JobID     string `json:"jobId"`
	RepoID    string `json:"repoId"`
	CreatedAt string `json:"createdAt"`
}

// Pending restore request from dashboard
type PendingRestore struct {
	JobID        string   `json:"jobId"`
	SnapshotID   string   `json:"snapshotId"`
	TargetPath   string   `json:"targetPath"`
	IncludePaths []string `json:"includePaths"`
	ExcludePaths []string `json:"excludePaths"`
	CreatedAt    string   `json:"createdAt"`
}

// Heartbeat
type HeartbeatRequest struct {
	AgentVersion  string `json:"agent_version"`
	ResticVersion string `json:"restic_version"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	Hostname      string `json:"hostname"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	LastBackupAt  string `json:"last_backup_at,omitempty"`
	DiskFreeBytes int64  `json:"disk_free_bytes"`
	CPUCount      int    `json:"cpu_count"`
	MemTotalBytes int64  `json:"memory_total_bytes"`
}

type HeartbeatResponse struct {
	ConfigChanged bool   `json:"config_changed"`
	ConfigHash    string `json:"config_hash"`
}

// Job report
type JobReportRequest struct {
	RepoID           string    `json:"repo_id"`
	PolicyID         string    `json:"policy_id,omitempty"`
	Operation        string    `json:"operation"`
	Status           string    `json:"status"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	ResticSnapshotID string    `json:"restic_snapshot_id,omitempty"`
	Stats            JobStats  `json:"stats"`
	ErrorMessage     string    `json:"error_message,omitempty"`
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

// Progress report (sent during backup)
type ProgressReport struct {
	RepoID         string  `json:"repo_id"`
	PercentDone    float64 `json:"percent_done"`
	BytesProcessed int64   `json:"bytes_processed"`
	FilesProcessed int     `json:"files_processed"`
	CurrentFile    string  `json:"current_file,omitempty"`
	StartedAt      string  `json:"started_at"`
}

// Log batch
type LogBatch struct {
	Lines []string `json:"lines"`
}

// Version info
type VersionInfo struct {
	Version   string            `json:"version"`
	Platforms map[string]string `json:"platforms"`
}

// Repo config (returned by GET /agents/:id/repos — password decrypted server-side)
type RepoConfig struct {
	ID                 string               `json:"id"`
	StorageBackendID   string               `json:"storage_backend_id"`
	PolicyID           string               `json:"policy_id,omitempty"`
	ResticRepoPath     string               `json:"restic_repo_path"`
	ResticPassword     string               `json:"restic_password"`
	Paths              []string             `json:"paths"`
	ExcludePatterns    []string             `json:"exclude_patterns"`
	ExcludePresets     []string             `json:"exclude_presets"`
	Tags               []string             `json:"tags"`
	ScheduleCron       string               `json:"schedule_cron,omitempty"`
	BandwidthLimitKiB  int                  `json:"bandwidth_limit_kib,omitempty"`
	PreBackupCommand   string               `json:"pre_backup_command,omitempty"`
	PostBackupCommand  string               `json:"post_backup_command,omitempty"`
	CheckEveryNBackups int                  `json:"check_every_n_backups"`
	Preset             string               `json:"preset,omitempty"` // "full-system", "docker-stop", "docker-hot"
	StorageConfig      StorageBackendConfig `json:"storage_config"`
	ConfigHash         string               `json:"config_hash,omitempty"`
}

// SystemMetadata is captured by the full-system preset pre-hook.
type SystemMetadata struct {
	BootMode        string   `json:"boot_mode"`         // "UEFI" or "BIOS"
	Hostname        string   `json:"hostname"`
	Kernel          string   `json:"kernel"`
	Packages        []string `json:"packages,omitempty"` // installed package names
	EnabledServices []string `json:"enabled_services,omitempty"`
	DiskUsage       string   `json:"disk_usage,omitempty"`
}

type StorageBackendConfig struct {
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	AccessKeyID    string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}
