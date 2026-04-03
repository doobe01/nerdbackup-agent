# NerdBackup Agent

Lightweight Go agent that wraps [Restic](https://restic.net) for local file and on-prem backup, managed by the [NerdBackup](https://nerdbackup.com) platform.

## What it does

The agent is **not** a backup engine — Restic handles all chunking, deduplication, encryption, and storage. The agent is an orchestration shim that:

1. Authenticates with the NerdBackup API
2. Pulls configuration (paths, schedules, storage backends, excludes)
3. Translates config into `restic` CLI invocations
4. Parses Restic's JSON output for progress and status
5. Reports results back to the NerdBackup API
6. Sends heartbeats so the dashboard shows agent status

## Install

```bash
curl -sSL https://nerdbackup.com/install.sh | sh
```

Or download from [Releases](https://github.com/doobe01/nerdbackup-agent/releases).

## Usage

```bash
# Register with NerdBackup
nerdbackup-agent init --api-key nb_live_xxxxx

# Start agent (heartbeat + scheduled backups)
nerdbackup-agent run

# Trigger immediate backup
nerdbackup-agent backup

# List snapshots
nerdbackup-agent snapshots

# Show status
nerdbackup-agent status
```

## How it works

- All communication is **outbound HTTPS** — no firewall rules needed
- Config changes made in the dashboard sync to the agent every 5 minutes
- Backups are reported as jobs in the NerdBackup dashboard alongside SaaS backups

## Attribution

NerdBackup Agent is powered by [Restic](https://restic.net), licensed under BSD 2-Clause. See [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) for details.

## License

MIT
