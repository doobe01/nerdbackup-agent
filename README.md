# NerdBackup Agent

[![CI](https://github.com/doobe01/nerdbackup-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/doobe01/nerdbackup-agent/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/doobe01/nerdbackup-agent)](https://goreportcard.com/report/github.com/doobe01/nerdbackup-agent)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/doobe01/nerdbackup-agent)](go.mod)
[![Release](https://img.shields.io/github/v/release/doobe01/nerdbackup-agent)](https://github.com/doobe01/nerdbackup-agent/releases)

> Lightweight Go agent that wraps [Restic](https://restic.net) for local file and on-prem backup, managed by the [NerdBackup](https://nerdbackup.com) platform.

---

## Why NerdBackup Agent?

| | Raw Restic + Cron | NerdBackup Agent |
|---|---|---|
| **Setup** | Write bash scripts, configure cron, manage env vars | `nerdbackup-agent init --api-key KEY` |
| **Scheduling** | Cron on each server, no central view | Managed from dashboard, config syncs every 5 min |
| **Monitoring** | Parse logs, build your own alerts | Dashboard shows status, webhooks on failure |
| **Multi-server** | Each server is independent | All agents visible in one dashboard |
| **Updates** | Manual on each machine | `nerdbackup-agent update` checks for new versions |
| **Restore** | Remember the right env vars and flags | `nerdbackup-agent restore SNAPSHOT /target` |

The agent is **not** a backup engine — Restic handles all chunking, deduplication, AES-256 encryption, and storage I/O. The agent adds orchestration: scheduling, progress reporting, health checks, and a management API.

---

## Demo

```
$ nerdbackup-agent doctor
 ✓ Config file exists                          OK — /home/user/.nerdbackup/config.json
 ✓ Agent token valid                           OK
 ✓ API reachable (42ms)                        OK
 ✓ Restic binary found                         OK — /usr/local/bin/restic
 ✓ Restic version                              OK — restic 0.17.3
 ✓ Fetch repo config                           OK — 2 repos
 ✓ Repo 'abc123': path /home/user/documents    OK
 ✓ Repo 'abc123': storage accessible           OK
 ✓ Repo 'abc123': disk writable                OK

 9/9 checks passed

$ nerdbackup-agent backup
  12.4% done, 847 files
  48.7% done, 3291 files
  100.0% done, 12847 files
 Backup completed: snapshot a1b2c3d4, 42 new files, 52MB added
```

---

## Install

**Linux / macOS:**
```bash
curl -sSL https://nerdbackup.com/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://nerdbackup.com/install.ps1 | iex
```

**From source:**
```bash
go install github.com/doobe01/nerdbackup-agent/cmd/nerdbackup-agent@latest
```

**Or download** from [Releases](https://github.com/doobe01/nerdbackup-agent/releases).

---

## Quick Start

```bash
# 1. Register with NerdBackup (uses your API key for initial auth)
nerdbackup-agent init --api-key nb_live_xxxxx

# 2. Start the agent (heartbeat + scheduled backups)
nerdbackup-agent run

# 3. Or install as a system service
nerdbackup-agent install-service
```

---

## Commands

| Command | Description |
|---------|-------------|
| `init --api-key KEY` | Register agent with NerdBackup |
| `run` | Start agent daemon (heartbeat + scheduler) |
| `backup [--repo ID]` | Trigger immediate backup |
| `restore SNAPSHOT TARGET` | Restore a snapshot to a directory |
| `snapshots` | List all restic snapshots |
| `status` | Show agent config and status |
| `doctor` | Run diagnostic checks on the setup |
| `update` | Check for available agent updates |
| `install-service` | Install as systemd/launchd/scheduled task |
| `docker-discover` | Discover Docker volumes and Compose projects |

---

## Features

- **Retry with backoff** — API calls retry up to 5x (1s → 60s exponential with jitter)
- **Pending reports** — failed job reports saved to disk, retried on next startup
- **ETag config sync** — efficient polling, only rebuilds cron on actual changes
- **Stale lock removal** — removes restic locks >30 min before each backup
- **Auto-init repos** — new repos automatically initialized on first sync
- **Graceful shutdown** — waits for in-flight backups on SIGTERM/SIGINT
- **Snapshot tagging** — every backup tagged with `nerdbackup:agent_id`, `nerdbackup:repo_id`, `nerdbackup:hostname`
- **Progress reporting** — real-time progress sent to API every 10s
- **Health checks** — `restic check` every N backups (configurable)
- **Bandwidth throttling** — `--limit-upload` / `--limit-download`
- **Pre/post hooks** — shell commands before/after backup with env vars
- **Exclude presets** — built-in patterns for `developer`, `macos`, `windows`, `full-system`
- **Docker discovery** — finds volumes and Compose projects for backup config
- **Full system backup** — preset with disaster recovery metadata capture

---

## How It Works

```
┌─────────────────────┐                 ┌──────────────────────┐
│  nerdbackup-agent   │  ──HTTPS──►    │  NerdBackup API      │
│  ├─ Scheduler       │  heartbeat     │  (nerdbackup.com)    │
│  ├─ Restic Runner   │  job reports   │                      │
│  └─ Config Sync     │  config pull   │  Dashboard shows     │
│         │           │                 │  agent status +      │
│    exec restic      │                 │  backup history      │
│         ▼           │                 └──────────────────────┘
│  ┌───────────────┐  │
│  │    Restic     │──────S3/B2/etc──► User's Storage Bucket
│  └───────────────┘  │
└─────────────────────┘
```

All communication is **outbound HTTPS** — no firewall rules, no port forwarding, no VPN required. Config changes made in the dashboard sync to the agent every 5 minutes.

---

## Configuration

The agent stores its config at `~/.nerdbackup/config.json`:

```json
{
  "agent_id": "uuid",
  "agent_token": "nb_agent_xxxxx",
  "api_base_url": "https://nerdbackup.com",
  "name": "prod-web-01",
  "debug": false,
  "initialized_repos": ["repo-1", "repo-2"],
  "last_backup_at": "2026-04-05T02:15:00Z"
}
```

Backup paths, schedules, excludes, and hooks are managed from the NerdBackup dashboard and synced to the agent automatically.

---

## Supported Platforms

| OS | Arch | Binary | Service |
|---|---|---|---|
| Linux | amd64, arm64 | tar.gz | systemd |
| macOS | amd64, arm64 | tar.gz | launchd |
| Windows | amd64 | zip | scheduled task |

---

## Development

```bash
# Prerequisites: Go 1.22+, Restic

# Build
make build

# Run tests
make test

# Run all checks (fmt, vet, lint, test)
make check

# Test coverage
make coverage

# Build for all platforms
make build GOOS=linux GOARCH=amd64
make build GOOS=darwin GOARCH=arm64
make build GOOS=windows GOARCH=amd64
```

---

## Documentation

- **Agent Guide:** [nerdbackup.com/docs/agent](https://nerdbackup.com/docs/agent)
- **API Reference:** [nerdbackup.com/docs/api](https://nerdbackup.com/docs/api)
- **Getting Started:** [nerdbackup.com/docs/getting-started](https://nerdbackup.com/docs/getting-started)

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Please note that this project has a [Code of Conduct](CODE_OF_CONDUCT.md).

---

## Security

To report a security vulnerability, see [SECURITY.md](SECURITY.md).

---

## Attribution

NerdBackup Agent is powered by [Restic](https://restic.net), licensed under BSD 2-Clause. See [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) for details.

---

## Related

- **[NerdBackup Platform](https://github.com/doobe01/nerdbackup.com)** — API, dashboard, billing, SaaS connectors

---

## License

[MIT](LICENSE)
