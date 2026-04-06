# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in NerdBackup Agent, please report it responsibly.

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, email **security@nerdbackup.com** with:

1. Description of the vulnerability
2. Steps to reproduce
3. Potential impact
4. Suggested fix (if any)

We will acknowledge your report within 48 hours and aim to release a fix within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Previous release | Security fixes only |
| Older | No |

## Security Considerations

The NerdBackup Agent:

- Stores its auth token in `~/.nerdbackup/config.json` with `0600` permissions
- Passes restic passwords via environment variables (standard restic practice)
- Executes pre/post backup hooks via `sh -c` — hooks come from the NerdBackup API and should only be configured by trusted admins
- All API communication is over HTTPS
- Config saves are atomic (write to .tmp, then rename) to prevent corruption

## Scope

In scope:
- Authentication bypass
- Credential exposure
- Command injection via hook execution
- Privilege escalation
- Data exfiltration

Out of scope:
- Restic vulnerabilities (report to [restic/restic](https://github.com/restic/restic))
- Social engineering
- Denial of service on the agent binary
