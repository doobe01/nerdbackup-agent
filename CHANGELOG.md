# Changelog

All notable changes to the NerdBackup Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- Replaced `autoInitRepo` with `ensureRepoReady` — always verifies repo accessibility before backup (never trusts local state), clears stale locks first, and verifies init succeeded
- `runBackup` now calls `ensureRepoReady` before every backup with proper error handling and dashboard status reporting on failure
- `syncAndSchedule` uses `ensureRepoReady` for each repo during config sync (logs and continues if one fails)
- `HandleCommand("start_backup")` uses `ensureRepoReady` with error handling — reports failure to dashboard if repo is not ready

### Added
- `ForgetSnapshot` method on restic Runner: removes a specific snapshot by ID without pruning
- `Prune` method on restic Runner: removes unreferenced data from the repository
- `forget_snapshot` command handler in scheduler: forgets a specific snapshot, prunes unreferenced data, and reports completion via WebSocket
- Repo unlock after cancel: when a backup is cancelled, the agent now unlocks the restic repo to prevent stale locks from blocking future backups

### Removed
- `autoInitRepo` function (replaced by `ensureRepoReady` with proper error handling and verification)

## [0.5.5] - 2026-04-07

### Added
- Real cancel command — kills restic process via context cancellation
- Force config sync when start_backup arrives with no cached repos

### Fixed
- Backup no longer fails after creating new policy (force sync fetches new repo)
- Cancel from dashboard actually stops the running backup

## [0.5.4] - 2026-04-07

### Fixed
- Agent sends `job_started` message when backup begins (server updates job to "running")
- Jobs no longer stuck at "pending" — status reflects actual agent state

## [0.5.3] - 2026-04-07

### Fixed
- Uninstaller now kills `restic.exe` during uninstall (was still running mid-backup)
- Auto-init restic repo before WebSocket-triggered backup

## [0.5.2] - 2026-04-07

### Fixed
- Auto-init restic repo before WebSocket-triggered backup (fixes "repository does not exist" on first backup from dashboard)

## [0.5.1] - 2026-04-07

### Fixed
- WebSocket client: only dispatch messages with `type: "command"` — ignore heartbeat_ack, job_report_ack
- No more "Unknown command action" warnings in logs

## [0.5.0] - 2026-04-07

### Added
- Real-time WebSocket connection to NerdBackup server (replaces 5-min polling)
- Persistent connection with exponential backoff reconnect (1s→60s with jitter)
- Progress streaming over WebSocket every 5s during backup
- Instant command delivery: start_backup, pause, cancel, resume, config_update
- Falls back to HTTP polling when WebSocket unavailable
- Job reports prefer WebSocket with HTTP fallback
- Heartbeat over WebSocket (HTTP fallback when disconnected)

## [0.3.3] - 2026-04-07

### Fixed
- Service logging: diagnostic marker file + fallback log locations
- Init logging before config load so service errors are captured

## [0.3.2] - 2026-04-07

### Fixed
- Installer no longer hangs at end of install/uninstall (use nowait for service start and API deregister)

## [0.3.1] - 2026-04-07

### Fixed
- Windows Service logging: init logging before config load so errors are captured
- File permissions: grant LOCAL SYSTEM write access via icacls
- Inno Setup: use everyone-modify on ProgramData directory

## [0.3.0] - 2026-04-07

### Added
- Single file download from dashboard — agent runs `restic dump` and uploads file content to server
- `Dump()` method on restic runner
- `GetPendingFileDumps()` and `UploadFileDump()` API methods
- `checkPendingFileDumps()` in scheduler (polls alongside restores/backups)

### Changed
- README: replaced "scheduled task" with "Windows Service" for Windows platform
- README: added `service install|uninstall|start|stop` and `uninstall` commands to Commands table
- README: added `--install-token` flag documentation for `init` command
- README: added file logging paths (Windows/Linux/macOS) and auto-update documentation
- README: added dashboard zero-touch install flow to Install section
- installer/README: added `/API_URL` parameter to silent install docs
- installer/README: clarified that agent binary must exist in `installer/dist/` before building

## [0.2.9] - 2026-04-06

### Added
- Silent auto-update: agent checks for new versions hourly, downloads and swaps binary automatically
- Service restarts after update (Windows Service Manager / systemd / launchd handle restart)
- Config, agent ID, tokens, and repos are preserved during updates — only the binary changes

### Fixed
- File logging: sync after each write to ensure logs flush immediately
- Log rotation: renames to `.old` at 10MB instead of in-place truncation

## [0.2.8] - 2026-04-06

### Fixed
- Dashboard-triggered backups no longer create duplicate job entries — passes `dashboard_job_id` to update existing pending job

## [0.2.7] - 2026-04-06

### Added
- File listing captured after each backup (restic ls, max 500 files) and included in job report
- Dashboard can now browse snapshot contents for new backups
- `LsFiles()` method on restic runner

## [0.2.6] - 2026-04-06

### Added
- Local file logging: writes to `C:\ProgramData\NerdBackup\agent.log` (Windows) or `~/.nerdbackup/agent.log` (Linux/macOS)
- Logs to both console and file simultaneously
- Auto-truncates log file at 10MB

## [0.2.5] - 2026-04-06

### Fixed
- Pending restores and backups now checked on EVERY sync tick (not only when config changes)
- Dashboard "Run" button now triggers agent backups via Redis queue

### Added
- `checkPendingBackups()` — agent polls for dashboard-triggered backup requests
- `GetPendingBackups()` API method + `PendingBackup` type

## [0.2.4] - 2026-04-06

### Added
- One-click restore from dashboard — agent polls for pending restore requests
- `GetPendingRestores()` API method
- Scheduler checks for pending restores on each config sync
- Executes `restic restore` and reports completion/failure back to server

## [0.2.3] - 2026-04-06

### Added
- `uninstall` command — deregisters from NerdBackup API, stops service, removes service, cleans up config
- Inno Setup uninstaller now calls `uninstall` to fully clean up (agent disappears from dashboard)

## [0.2.2] - 2026-04-06

### Fixed
- Restic finder checks next to agent binary first (fixes Windows Service / LOCAL SYSTEM)
- On Windows, restic auto-downloads to agent's directory (e.g. `C:\Program Files\NerdBackup\`) instead of user home

## [0.2.1] - 2026-04-06

### Fixed
- Windows Service: copy config to `%PROGRAMDATA%\NerdBackup\` during service install so LOCAL SYSTEM can read it
- Config store checks system-wide path first on Windows (fallback to user home)

## [0.2.0] - 2026-04-06

### Added
- Professional Windows installer (Inno Setup) with wizard UI, activation code page, UAC elevation
- Proper Windows Service via `golang.org/x/sys/windows/svc` (replaces Scheduled Task)
- `service install|uninstall|start|stop` subcommands for Windows service management
- Service auto-restarts on failure (10s, 30s, 60s recovery actions)
- Appears in Add/Remove Programs with clean uninstaller
- PATH modification, Start Menu shortcuts
- Support for silent install: `setup.exe /VERYSILENT /INSTALL_TOKEN=xxx`
- `Uninstall()`, `Start()`, `Stop()` functions for all platforms (Linux systemd, macOS launchd, Windows service)

### Removed
- Custom Go installer binary (`cmd/nerdbackup-installer`) — replaced by Inno Setup
- Scheduled Task service install — replaced by proper Windows Service

## [0.1.3] - 2026-04-06

### Fixed
- Windows installer: fix "file already closed" error when extracting zip downloads

## [0.1.2] - 2026-04-06

### Added
- Zero-touch install: `--install-token` flag for `init` command — auto-registers, installs restic, installs service, starts agent
- `RegisterWithToken` API method for pre-authenticated registration
- Windows .exe installer (`nerdbackup-installer.exe`) with appended config — download from dashboard, double-click, done

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
