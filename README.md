# adblocker

A system-wide DNS-based ad blocker for Linux. Blocks ads, trackers, and malware domains across **every application** — browsers, Electron apps, CLI tools, games — with no browser extensions or proxy certificates required.

It works by running a local DNS server on `127.0.0.1:53`. Queries for known ad domains are answered with `0.0.0.0` (instant connection refused). Everything else is forwarded to a real upstream resolver.

Blocklist sources (auto-updated every 24 hours, ~242k domains):
- [StevenBlack unified hosts](https://github.com/StevenBlack/hosts) — ads + malware
- [AdGuard DNS filter](https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt)
- [OISD basic](https://abp.oisd.nl/basic/)

---

## Requirements

- Linux (systemd-based distro recommended, but not required)
- Go 1.21+ (only needed to build from source)
- `dig` (optional, for testing — part of `dnsutils`)
- Root / `sudo` to bind port 53 and modify DNS config

---

## Install

### 1. Build

```bash
git clone https://github.com/lucasnobrega98/adblocker
cd adblocker
go build -o adblocker ./cmd/adblocker
```

### 2. Install system-wide (one command)

```bash
sudo ./install.sh
```

This will:
1. Copy the binary to `/usr/local/bin/adblocker`
2. Create `/var/lib/adblocker/` for blocklist data
3. Install and enable the systemd service (`adblocker.service`)
4. Download blocklists (~242k domains)
5. Point your system DNS at `127.0.0.1`
6. Start the service

### 3. Verify it's working

```bash
# Should return 0.0.0.0
dig doubleclick.net A +short

# Should return a real IP
dig github.com A +short

# Check service status
systemctl status adblocker

# Open the stats dashboard
xdg-open http://127.0.0.1:8080
```

---

## Manual install (without the script)

If you prefer to do it step by step:

```bash
# Install binary
sudo install -m 755 adblocker /usr/local/bin/adblocker

# Create data directory
sudo mkdir -p /var/lib/adblocker

# Download blocklists
sudo adblocker update

# Install and start the systemd service
sudo install -m 644 adblocker.service /etc/systemd/system/adblocker.service
sudo systemctl daemon-reload
sudo systemctl enable --now adblocker

# Point system DNS at the local server
sudo adblocker dns-on
sudo systemctl restart systemd-resolved   # if using systemd-resolved
```

---

## Usage

```
adblocker <command> [flags]

Commands:
  run         Start the DNS server (+ web dashboard)
  update      Download / refresh blocklists
  whitelist   Manage per-domain exceptions

  dns-on      Point system DNS at the local server  (requires sudo)
  dns-off     Restore original system DNS           (requires sudo)
```

### `run` — start the server

```bash
sudo adblocker run

# Custom options
sudo adblocker run \
  -listen   127.0.0.1:53   \  # DNS listen address
  -upstream 1.1.1.1:53     \  # use Cloudflare instead of Google
  -web      127.0.0.1:8080 \  # web dashboard address (default)
  -no-web                  \  # disable the dashboard
  -data     /var/lib/adblocker \
  -ttl      24                 # hours before blocklist is refreshed
```

The web dashboard is available at **http://127.0.0.1:8080** while the server is running. It auto-refreshes every 3 seconds and shows:

- Total blocked / allowed / error counts
- Block rate (%) and average queries per second
- Top 20 blocked domains with hit counts
- Top 20 allowed domains
- Live recent-blocks feed
- Query distribution donut chart

Port 53 requires root or the `CAP_NET_BIND_SERVICE` capability. If you want to run without root, use a high port and redirect with iptables:

```bash
adblocker run -listen 127.0.0.1:5353 &

# Redirect :53 → :5353 (add to /etc/rc.local or a systemd unit)
sudo iptables -t nat -A OUTPUT -p udp --dport 53 -j REDIRECT --to-port 5353
sudo iptables -t nat -A OUTPUT -p tcp --dport 53 -j REDIRECT --to-port 5353
```

### `update` — refresh blocklists

Downloads all sources and rebuilds the cache. Useful to run via cron or when you want fresh lists without restarting the server.

```bash
sudo adblocker update
```

The server does **not** need to be restarted after an update — it reads the new cache on next startup. To reload live, restart the service:

```bash
sudo systemctl restart adblocker
```

### `whitelist` — allow specific domains

If a site you need is broken (e.g. a payment provider, a CDN you depend on), whitelist it:

```bash
# Allow a domain
adblocker whitelist add spotify.com

# Remove it again
adblocker whitelist remove spotify.com

# See all exceptions
adblocker whitelist list
```

Whitelisted domains take effect on the **next server start**. Restart the service after changes:

```bash
sudo systemctl restart adblocker
```

Whitelist is stored at `/var/lib/adblocker/whitelist.txt` (one domain per line) and can be edited directly.

### `dns-on` / `dns-off` — system DNS toggle

```bash
# Enable: routes all system DNS queries through adblocker
sudo adblocker dns-on

# Disable: restores original DNS
sudo adblocker dns-off
```

On systemd-resolved systems, this writes a drop-in config to `/etc/systemd/resolved.conf.d/adblocker.conf` and sets `DNSStubListener=no` (so resolved vacates port 53). On other systems it prepends `nameserver 127.0.0.1` to `/etc/resolv.conf`, saving a backup first.

---

## Systemd service

The included `adblocker.service` runs the server as a sandboxed unit (no new privileges, read-only filesystem except `/var/lib/adblocker`).

```bash
sudo systemctl start   adblocker   # start now
sudo systemctl stop    adblocker   # stop
sudo systemctl restart adblocker   # restart (e.g. after whitelist changes)
sudo systemctl enable  adblocker   # start on boot
sudo systemctl disable adblocker   # don't start on boot

# View live logs
journalctl -u adblocker -f
```

---

## Configuration reference

| Flag | Default | Description |
|---|---|---|
| `-listen` | `127.0.0.1:53` | Address the DNS server binds to |
| `-upstream` | `8.8.8.8:53` | Upstream resolver for allowed queries |
| `-web` | `127.0.0.1:8080` | Web dashboard listen address |
| `-no-web` | `false` | Disable the web dashboard |
| `-data` | `/var/lib/adblocker` | Directory for blocklist cache and whitelist |
| `-ttl` | `24` | Hours before the blocklist cache is considered stale |

**Environment variable:** `ADBLOCKER_DATA` overrides the data directory (useful for running without root — falls back to `~/.local/share/adblocker` automatically when not running as root).

---

## Uninstall

```bash
# Stop and disable the service
sudo systemctl stop adblocker
sudo systemctl disable adblocker

# Restore system DNS
sudo adblocker dns-off
sudo systemctl restart systemd-resolved   # if applicable

# Remove files
sudo rm /usr/local/bin/adblocker
sudo rm /etc/systemd/system/adblocker.service
sudo rm -rf /var/lib/adblocker
sudo systemctl daemon-reload
```

---

## How it works

All DNS queries from every application go through the OS resolver. After running `dns-on`, the OS resolver is pointed at `127.0.0.1:53`. The adblocker process listens there, checks each queried domain against the blocklist, and either:

- **Blocks it:** returns `0.0.0.0` (IPv4) or `::` (IPv6). The calling application immediately gets `ECONNREFUSED` when it tries to open a TCP connection — no ad content is ever fetched.
- **Allows it:** forwards the original DNS query to the upstream resolver (`8.8.8.8`) and passes the real answer back unchanged.

The blocklist is an in-memory hash set (~20 MB RAM for 250k domains). Lookups are O(1) and add no perceptible latency to allowed queries.

Every query — blocked or allowed — is recorded by a stats collector that tracks per-domain hit counts and a ring buffer of the last 50 blocked domains. The built-in web server at `http://127.0.0.1:8080` reads these metrics and serves a live dashboard that auto-refreshes every 3 seconds, with no external dependencies.

For details on the internal design see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Troubleshooting

**Port 53 already in use**

On most Ubuntu/Debian systems, `systemd-resolved` holds port 53. `dns-on` handles this by setting `DNSStubListener=no` in the resolved config. If you skipped `dns-on`, run it and restart resolved:

```bash
sudo adblocker dns-on
sudo systemctl restart systemd-resolved
```

**A site I need is broken**

Whitelist it:

```bash
adblocker whitelist add example.com
sudo systemctl restart adblocker
```

The easiest way to find which domain is being blocked is the dashboard's **Recent Blocks** feed at `http://127.0.0.1:8080` — it updates live every 3 seconds. For a historical view, check the **Top Blocked Domains** table on the same page.

Alternatively, check the logs:

```bash
journalctl -u adblocker -f
# Look for lines like: [BLOCK] cdn.example.com.
```

**DNS stopped working after a crash**

If the service crashes while system DNS is pointing at `127.0.0.1`, DNS resolution will fail system-wide. Either restart the service or restore DNS temporarily:

```bash
sudo systemctl start adblocker
# or restore DNS directly:
sudo adblocker dns-off
```

**Blocklist not updating**

Force a manual update:

```bash
sudo adblocker update
sudo systemctl restart adblocker
```

The cache file is at `/var/lib/adblocker/blocklist.cache`. You can inspect or `grep` it directly:

```bash
grep "doubleclick" /var/lib/adblocker/blocklist.cache
```
=======
# adsink
System-wide Ad sink for linux
