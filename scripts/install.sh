#!/bin/bash
set -euo pipefail

# NerdBackup Agent installer for Linux and macOS
# Usage: curl -sSL https://nerdbackup.com/install.sh | sh

REPO="doobe01/nerdbackup-agent"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="nerdbackup-agent"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "==> NerdBackup Agent Installer"
echo "    OS: $OS, Arch: $ARCH"

# Get latest release
LATEST=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "Error: Could not determine latest release"
  exit 1
fi

echo "==> Downloading nerdbackup-agent v$LATEST"

TARBALL="${BINARY_NAME}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${LATEST}/${TARBALL}"

mkdir -p "$INSTALL_DIR"
curl -sSL "$URL" | tar -xz -C "$INSTALL_DIR" "$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

echo "==> Installed to $INSTALL_DIR/$BINARY_NAME"

# Check if in PATH
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "Add to your PATH:"
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

echo "==> Next steps:"
echo "  1. nerdbackup-agent init --api-key YOUR_API_KEY"
echo "  2. nerdbackup-agent run"
echo ""
echo "Done!"
