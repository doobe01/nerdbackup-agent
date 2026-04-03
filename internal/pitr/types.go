package pitr

// PITRConfig defines the configuration for point-in-time recovery.
type PITRConfig struct {
	DatabaseType       string `json:"database_type"` // "postgresql" or "mysql"
	ConnectionHost     string `json:"connection_host"`
	ConnectionPort     int    `json:"connection_port"`
	DatabaseName       string `json:"database_name"`
	User               string `json:"user"`
	Password           string `json:"password"`
	WALArchiveDir      string `json:"wal_archive_dir"`
	BaseBackupDir      string `json:"base_backup_dir"`
	WALArchiveInterval int    `json:"wal_archive_interval"` // seconds
	BaseBackupCron     string `json:"base_backup_cron"`
}

// WALStatus reports the current state of WAL archiving.
type WALStatus struct {
	LastWALArchived    string `json:"last_wal_archived"`
	WALArchiveCount    int    `json:"wal_archive_count"`
	LastBaseBackup     string `json:"last_base_backup"`
	CurrentRPOSeconds  int    `json:"current_rpo_seconds"`
	ArchiveDirSizeBytes int64 `json:"archive_dir_size_bytes"`
}
