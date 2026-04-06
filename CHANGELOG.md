# Changelog

All notable changes to the NerdBackup Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.2] - 2026-04-06

### Added
- Zero-touch install: `--install-token` flag for `init` command — auto-registers, installs restic, installs service, starts agent
- `RegisterWithToken` API method for pre-authenticated registration

### Fixed
- Restic auto-installer now extracts `.bz2`/`.zip` archives instead of saving raw archive as binary
- Windows: install restic as `restic.exe` (was missing `.exe` extension)

## [0.1.1] - 2026-04-06

### Added
- 6 new exclude presets: `golang`, `rust`, `ruby`, `docker`, `kubernetes`, `database` (10 total)
- Benchmarks for preset loading, config round-trip, retry logic
- MIT LICENSE file
- CONTRIBUTING.md, CODE_OF_CONDUCT.md, SECURITY.md
- GitHub Actions CI (test + lint + build on all platforms)
- GitHub Actions release workflow (GoReleaser on tag push)
- Issue templates (bug report, feature request) and PR template
- .editorconfig for consistent formatting
- Unit tests: config store (save/load/atomic/exists), retry logic, presets

### Fixed
- macOS: `launchctl load` replaced with modern `launchctl bootstrap` (with legacy fallback)
- Windows: disk free space now checks home directory drive instead of hardcoded C:\
- `getResticVersion`: properly parses version number from restic output, 5s timeout
- All unchecked error returns fixed (golangci-lint errcheck clean)
- `.gitignore`: no longer ignores `cmd/nerdbackup-agent/` source directory
- Docker discovery now uploads volumes to NerdBackup API
- `update` command description corrected to "Check for available agent updates"

### Changed
- Go dependencies downgraded for Go 1.23 compatibility (zerolog v1.33, x/sys v0.25)
- Makefile: added `fmt`, `vet`, `coverage`, `install`, `release`, `check` targets

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
