# NerdBackup Agent installer for Windows
# Usage: irm https://nerdbackup.com/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "doobe01/nerdbackup-agent"
$installDir = "$env:LOCALAPPDATA\nerdbackup"
$binaryName = "nerdbackup-agent.exe"

Write-Host "==> NerdBackup Agent Installer (Windows)"

# Get latest release
$release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$version = $release.tag_name -replace '^v', ''
Write-Host "==> Downloading nerdbackup-agent v$version"

$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$zipName = "nerdbackup-agent_windows_$arch.zip"
$downloadUrl = "https://github.com/$repo/releases/download/v$version/$zipName"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$zipPath = "$env:TEMP\$zipName"

Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath
Expand-Archive -Path $zipPath -DestinationPath $installDir -Force
Remove-Item $zipPath

Write-Host "==> Installed to $installDir\$binaryName"

# Add to PATH if not already there
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$installDir", "User")
    Write-Host "==> Added $installDir to PATH (restart terminal to apply)"
}

Write-Host ""
Write-Host "==> Next steps:"
Write-Host "  1. nerdbackup-agent init --api-key YOUR_API_KEY"
Write-Host "  2. nerdbackup-agent run"
Write-Host ""
Write-Host "Done!"
