#!/usr/bin/env bash
# Installs adsink system-wide and enables the systemd service.
set -e

if [ "$EUID" -ne 0 ]; then
  echo "Run with sudo: sudo ./install.sh"
  exit 1
fi

BINARY=./adsink
SERVICE=./adsink.service

if [ ! -f "$BINARY" ]; then
  echo "Binary not found. Build first: go build -o adsink ./cmd/adsink"
  exit 1
fi

echo "Installing binary to /usr/local/bin/adsink"
install -m 755 "$BINARY" /usr/local/bin/adsink

echo "Creating data directory /var/lib/adsink"
mkdir -p /var/lib/adsink

echo "Installing systemd service"
install -m 644 "$SERVICE" /etc/systemd/system/adsink.service
systemctl daemon-reload
systemctl enable adsink

echo ""
echo "Downloading blocklists..."
adsink update

echo ""
echo "Pointing system DNS at 127.0.0.1..."
adsink dns-on

echo ""
echo "Starting service..."
systemctl start adsink
systemctl restart systemd-resolved 2>/dev/null || true

echo ""
echo "Done. Check status with: systemctl status adsink"
