# Changelog

All notable changes to the NerdBackup Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-04-05

### Added
- Initial release
- 10 CLI commands: init, run, backup, restore, snapshots, status, doctor, update, install-service, docker-discover
- Restic-powered backup engine (BSD 2-Clause)
- Cross-platform support: Linux, macOS, Windows (amd64 + arm64)
- API client with retry logic and exponential backoff
- ETag-based config sync for efficient polling
- Pre/post backup hooks with environment variables
- Exclude presets: developer, macos, windows, full-system
- Docker volume and Compose project discovery
- PostgreSQL PITR module (WAL archiving, base backup)
- Full system backup preset with disaster recovery metadata capture
- Automatic stale lock removal before each backup
- Auto-initialization of new restic repos
- Graceful shutdown with context cancellation
- Bandwidth throttling (--limit-upload/--limit-download)
- Snapshot tagging with NerdBackup metadata
- Real-time progress reporting to API
- Periodic health checks (restic check every N backups)
- Pending report persistence (retried on restart)
- Atomic config saves (write-to-tmp + rename)
- systemd, launchd, and Windows scheduled task service installers
- Structured JSON logging via zerolog

### Powered By
- [Restic](https://restic.net) v0.17.3 — BSD 2-Clause License
