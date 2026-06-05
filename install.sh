#!/usr/bin/env bash
# Installs adblocker system-wide and enables the systemd service.
set -e

if [ "$EUID" -ne 0 ]; then
  echo "Run with sudo: sudo ./install.sh"
  exit 1
fi

BINARY=./adblocker
SERVICE=./adblocker.service

if [ ! -f "$BINARY" ]; then
  echo "Binary not found. Build first: go build -o adblocker ./cmd/adblocker"
  exit 1
fi

echo "Installing binary to /usr/local/bin/adblocker"
install -m 755 "$BINARY" /usr/local/bin/adblocker

echo "Creating data directory /var/lib/adblocker"
mkdir -p /var/lib/adblocker

echo "Installing systemd service"
install -m 644 "$SERVICE" /etc/systemd/system/adblocker.service
systemctl daemon-reload
systemctl enable adblocker

echo ""
echo "Downloading blocklists..."
adblocker update

echo ""
echo "Pointing system DNS at 127.0.0.1..."
adblocker dns-on

echo ""
echo "Starting service..."
systemctl start adblocker
systemctl restart systemd-resolved 2>/dev/null || true

echo ""
echo "Done. Check status with: systemctl status adblocker"
