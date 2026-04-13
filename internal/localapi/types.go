package localapi

// StatusProvider is implemented by the scheduler to expose state to the local API.
type StatusProvider interface {
	GetStatus() AgentStatus
	GetRepos() []RepoStatus
	GetProgress() *BackupProgress
	TriggerBackup(repoID string) error
}

type AgentStatus struct {
	Version       string `json:"version"`
	AgentID       string `json:"agent_id"`
	AgentName     string `json:"agent_name"`
	Online        bool   `json:"online"`        // WebSocket connected
	Uptime        string `json:"uptime"`
	LastBackupAt  string `json:"last_backup_at"`
	RepoCount     int    `json:"repo_count"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	ResticVersion string `json:"restic_version"`
	APIURL        string `json:"api_url"`
}

type RepoStatus struct {
	ID           string   `json:"id"`
	PolicyID     string   `json:"policy_id,omitempty"`
	Paths        []string `json:"paths"`
	ScheduleCron string   `json:"schedule_cron,omitempty"`
	LastBackupAt string   `json:"last_backup_at,omitempty"`
	Status       string   `json:"status"` // "idle", "running", "error"
}

type BackupProgress struct {
	RepoID      string  `json:"repo_id"`
	JobID       string  `json:"job_id,omitempty"`
	PercentDone float64 `json:"percent_done"`
	BytesDone   int64   `json:"bytes_done"`
	FilesDone   int     `json:"files_done"`
	CurrentFile string  `json:"current_file,omitempty"`
	StartedAt   string  `json:"started_at"`
}
