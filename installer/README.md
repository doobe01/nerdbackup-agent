# NerdBackup Agent Windows Installer

Built with [Inno Setup](https://jrsoftware.org/isinfo.php).

## Build

The installer is built automatically by GitHub Actions on tag push. To build manually:

1. Install [Inno Setup](https://jrsoftware.org/isdl.php) (v6.2+)
2. Build the agent binary into the `installer/dist/` directory:
   ```bash
   go build -o installer/dist/nerdbackup-agent.exe ./cmd/nerdbackup-agent
   ```
   **Important:** The Inno Setup script expects `nerdbackup-agent.exe` to exist in `installer/dist/` before building. The build will fail if this file is missing.
3. Run: `iscc /DMyAppVersion=0.1.0 installer/nerdbackup.iss`

Output: `installer/Output/nerdbackup-agent-0.1.0-windows-setup.exe`

## Silent Install

```powershell
nerdbackup-agent-setup.exe /VERYSILENT /SUPPRESSMSGBOXES /INSTALL_TOKEN=your_token /API_URL=https://nerdbackup.com
```

| Parameter | Description |
|---|---|
| `/VERYSILENT` | No UI at all |
| `/SUPPRESSMSGBOXES` | Suppress confirmation dialogs |
| `/INSTALL_TOKEN=xxx` | Pre-authenticated install token (from dashboard) |
| `/API_URL=https://custom.url` | Override the NerdBackup API URL (defaults to `https://nerdbackup.com`) |

## What the Installer Does

1. Copies `nerdbackup-agent.exe` to `C:\Program Files\NerdBackup\`
2. Adds to system PATH
3. Creates Start Menu shortcuts
4. Registers agent with NerdBackup (if activation code provided)
5. Installs and starts a Windows Service (`NerdBackupAgent`)
6. Adds entry to Add/Remove Programs

## Uninstall

Via Add/Remove Programs, or:

```powershell
"C:\Program Files\NerdBackup\unins000.exe" /VERYSILENT
```

The uninstaller stops the service, removes it, removes from PATH, and deletes files.
