package restic

import "time"

// ProgressEntry is emitted by restic --json during backup.
type ProgressEntry struct {
	MessageType    string   `json:"message_type"`
	SecondsElapsed float64  `json:"seconds_elapsed,omitempty"`
	PercentDone    float64  `json:"percent_done,omitempty"`
	TotalFiles     int      `json:"total_files,omitempty"`
	FilesDone      int      `json:"files_done,omitempty"`
	TotalBytes     int64    `json:"total_bytes,omitempty"`
	BytesDone      int64    `json:"bytes_done,omitempty"`
	CurrentFiles   []string `json:"current_files,omitempty"`
}

// BackupSummary is the final "summary" message from restic backup --json.
type BackupSummary struct {
	MessageType         string  `json:"message_type"`
	FilesNew            int     `json:"files_new"`
	FilesChanged        int     `json:"files_changed"`
	FilesUnmodified     int     `json:"files_unmodified"`
	DirsNew             int     `json:"dirs_new"`
	DirsChanged         int     `json:"dirs_changed"`
	DirsUnmodified      int     `json:"dirs_unmodified"`
	DataBlobs           int     `json:"data_blobs"`
	TreeBlobs           int     `json:"tree_blobs"`
	DataAdded           int64   `json:"data_added"`
	TotalFilesProcessed int     `json:"total_files_processed"`
	TotalBytesProcessed int64   `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
	SnapshotID          string  `json:"snapshot_id"`
}

// Snapshot represents a restic snapshot.
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Paths    []string  `json:"paths"`
	Hostname string    `json:"hostname"`
	Username string    `json:"username"`
	Tags     []string  `json:"tags"`
}

// RepoStats from restic stats.
type RepoStats struct {
	TotalSize      int64 `json:"total_size"`
	TotalFileCount int   `json:"total_file_count"`
}

// Lock represents a restic lock entry.
type Lock struct {
	Time      time.Time `json:"time"`
	Exclusive bool      `json:"exclusive"`
	Hostname  string    `json:"hostname"`
	Username  string    `json:"username"`
	PID       int       `json:"pid"`
	UID       int       `json:"uid"`
	GID       int       `json:"gid"`
}

// BackupOptions configures a backup run.
type BackupOptions struct {
	Paths             []string
	Excludes          []string
	Tags              []string
	BandwidthLimitKiB int
}
