# NerdBackup Agent

Lightweight Go binary that wraps [Restic](https://restic.net) for local file and on-prem backup, managed by the [NerdBackup](https://nerdbackup.com) platform.

## What It Does

The agent is **not** a backup engine — Restic handles all chunking, deduplication, AES-256 encryption, and storage I/O. The agent is a thin orchestration layer that:

1. Authenticates with the NerdBackup API
2. Pulls configuration (paths, schedules, storage backends, excludes)
3. Translates config into `restic` CLI invocations
4. Parses Restic's JSON output for progress and status
5. Reports results back to the NerdBackup API
6. Sends heartbeats so the dashboard shows agent online/offline status

All communication is **outbound HTTPS** — no firewall rules, no port forwarding, no VPN required.

## Install

**Linux / macOS:**
```bash
curl -sSL https://nerdbackup.com/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://nerdbackup.com/install.ps1 | iex
```

Or download from [Releases](https://github.com/doobe01/nerdbackup-agent/releases).

## Quick Start

```bash
# Register with NerdBackup (uses your API key for initial auth)
nerdbackup-agent init --api-key nb_live_xxxxx

# Start the agent (heartbeat + scheduled backups)
nerdbackup-agent run

# Or install as a system service
nerdbackup-agent install-service
```

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
| `update` | Check for agent updates |
| `install-service` | Install as systemd/launchd/scheduled task |
| `docker-discover` | Discover Docker volumes and Compose projects |

## Features

- **Retry with backoff** — API calls retry up to 5x (1s → 60s exponential)
- **Pending reports** — failed job reports saved locally, retried on next startup
- **ETag config sync** — efficient polling, only rebuilds cron on actual changes
- **Stale lock removal** — removes restic locks >30 min old before each backup
- **Auto-init repos** — new repos automatically initialized on first sync
- **Graceful shutdown** — waits for in-flight backups on SIGTERM/SIGINT
- **Snapshot tagging** — every backup tagged with `nerdbackup:agent_id`, `nerdbackup:repo_id`, `nerdbackup:hostname`
- **Progress reporting** — real-time progress sent to API every 10s
- **Health checks** — `restic check` every N backups (configurable)
- **Bandwidth throttling** — `--limit-upload` / `--limit-download`
- **Pre/post hooks** — shell commands before/after backup with env vars (`NERDBACKUP_STATUS`, `NERDBACKUP_SNAPSHOT_ID`, etc.)
- **Exclude presets** — built-in patterns for `developer`, `macos`, `windows`, `full-system`
- **Docker discovery** — finds volumes and Compose projects for backup config
- **Full system backup** — `full-system` preset with disaster recovery metadata capture

## Supported Platforms

| OS | Arch | Binary | Service |
|---|---|---|---|
| Linux | amd64, arm64 | tar.gz | systemd |
| macOS | amd64, arm64 | tar.gz | launchd |
| Windows | amd64 | zip | scheduled task |

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

Config changes made in the dashboard sync to the agent every 5 minutes. No SSH access to the machine required.

## Documentation

- **Agent Guide:** [nerdbackup.com/docs/agent](https://nerdbackup.com/docs/agent)
- **API Reference:** [nerdbackup.com/docs/api](https://nerdbackup.com/docs/api)
- **Getting Started:** [nerdbackup.com/docs/getting-started](https://nerdbackup.com/docs/getting-started)

## Attribution

NerdBackup Agent is powered by [Restic](https://restic.net), licensed under BSD 2-Clause. See [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) for details.

## Related

- **[NerdBackup Platform](https://github.com/doobe01/nerdbackup.com)** — API, dashboard, billing, SaaS connectors

## License

MIT
