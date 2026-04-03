#!/bin/bash
set -e

# NerdBackup Full System Backup — Pre-backup metadata capture
# This script captures system metadata needed for disaster recovery.
# The output directory is included in the restic backup snapshot.

META_DIR="/tmp/nerdbackup-system-meta"
mkdir -p "$META_DIR"

echo "Capturing system metadata for disaster recovery..."

# Partition layout
sfdisk -d /dev/sda > "$META_DIR/partition-table-sda.bak" 2>/dev/null || true
sfdisk -d /dev/vda > "$META_DIR/partition-table-vda.bak" 2>/dev/null || true
lsblk -f -J > "$META_DIR/block-devices.json" 2>/dev/null || true
blkid > "$META_DIR/blkid.txt" 2>/dev/null || true

# Bootloader info
if [ -d /sys/firmware/efi ]; then
  echo "UEFI" > "$META_DIR/boot-mode.txt"
  efibootmgr -v > "$META_DIR/efi-entries.txt" 2>/dev/null || true
else
  echo "BIOS" > "$META_DIR/boot-mode.txt"
fi

# Installed packages
dpkg --get-selections > "$META_DIR/packages-dpkg.txt" 2>/dev/null || true
rpm -qa --qf '%{NAME}\n' > "$META_DIR/packages-rpm.txt" 2>/dev/null || true
pacman -Qqe > "$META_DIR/packages-pacman.txt" 2>/dev/null || true

# Filesystem table
cp /etc/fstab "$META_DIR/fstab.bak" 2>/dev/null || true

# Network configuration
ip addr show > "$META_DIR/network-interfaces.txt" 2>/dev/null || true
ip route show > "$META_DIR/network-routes.txt" 2>/dev/null || true
cat /etc/resolv.conf > "$META_DIR/resolv.conf.bak" 2>/dev/null || true
cat /etc/hostname > "$META_DIR/hostname.txt" 2>/dev/null || true

# Enabled services
systemctl list-unit-files --type=service --state=enabled --no-pager > "$META_DIR/enabled-services.txt" 2>/dev/null || true

# Kernel info
uname -a > "$META_DIR/kernel.txt" 2>/dev/null || true

# Disk usage
df -h > "$META_DIR/disk-usage.txt" 2>/dev/null || true

# Crontabs
crontab -l > "$META_DIR/crontab-root.txt" 2>/dev/null || true
for user in $(cut -f1 -d: /etc/passwd); do
  crontab -u "$user" -l > "$META_DIR/crontab-$user.txt" 2>/dev/null || true
done

# SSH keys (public only, for reference)
ls -la /root/.ssh/ > "$META_DIR/ssh-keys-root.txt" 2>/dev/null || true

echo "System metadata captured to $META_DIR"
