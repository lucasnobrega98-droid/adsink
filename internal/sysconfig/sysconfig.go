// Package sysconfig manages system DNS configuration to point at the local server.
package sysconfig

import (
	"fmt"
	"os"
	"strings"
)

const resolvedConf = "/etc/systemd/resolved.conf.d/adsink.conf"
const resolvConf = "/etc/resolv.conf"

// Apply points the system DNS at addr (e.g. "127.0.0.1") using systemd-resolved if
// available, otherwise by prepending to /etc/resolv.conf. Requires root.
func Apply(addr string) error {
	if isSystemdResolved() {
		return applyResolved(addr)
	}
	return applyResolvConf(addr)
}

// Remove undoes the DNS configuration change.
func Remove() error {
	if isSystemdResolved() {
		return os.Remove(resolvedConf)
	}
	return removeFromResolvConf()
}

func isSystemdResolved() bool {
	_, err := os.Stat("/run/systemd/resolve/stub-resolv.conf")
	return err == nil
}

func applyResolved(addr string) error {
	if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0755); err != nil {
		return err
	}
	content := fmt.Sprintf("[Resolve]\nDNS=%s\nDNSStubListener=no\n", addr)
	if err := os.WriteFile(resolvedConf, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Println("Written", resolvedConf)
	fmt.Println("Run: sudo systemctl restart systemd-resolved")
	return nil
}

func applyResolvConf(addr string) error {
	existing, err := os.ReadFile(resolvConf)
	if err != nil {
		existing = []byte{}
	}
	if strings.Contains(string(existing), "# adsink") {
		return nil // already applied
	}
	backup := resolvConf + ".adsink.bak"
	_ = os.WriteFile(backup, existing, 0644)

	newContent := fmt.Sprintf("# adsink\nnameserver %s\n%s", addr, string(existing))
	return os.WriteFile(resolvConf, []byte(newContent), 0644)
}

func removeFromResolvConf() error {
	backup := resolvConf + ".adsink.bak"
	if _, err := os.Stat(backup); err == nil {
		return os.Rename(backup, resolvConf)
	}
	// Fallback: remove our lines manually
	content, err := os.ReadFile(resolvConf)
	if err != nil {
		return err
	}
	var lines []string
	skip := false
	for _, line := range strings.Split(string(content), "\n") {
		if line == "# adsink" {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(line, "nameserver") {
			skip = false
			continue
		}
		skip = false
		lines = append(lines, line)
	}
	return os.WriteFile(resolvConf, []byte(strings.Join(lines, "\n")), 0644)
}
