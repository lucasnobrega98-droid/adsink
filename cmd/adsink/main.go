package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lucasnobrega98/adsink/internal/blocklist"
	"github.com/lucasnobrega98/adsink/internal/dns"
	"github.com/lucasnobrega98/adsink/internal/stats"
	"github.com/lucasnobrega98/adsink/internal/sysconfig"
	"github.com/lucasnobrega98/adsink/internal/web"
)

const defaultDataDir = "/var/lib/adsink"
const defaultListen  = "127.0.0.1:53"
const defaultUpstream = "8.8.8.8:53"
const defaultWebAddr  = "127.0.0.1:8080"

func dataDir() string {
	if d := os.Getenv("adsink_DATA"); d != "" {
		return d
	}
	if os.Getuid() != 0 {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "adsink")
	}
	return defaultDataDir
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("[adsink] ")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		cmdRun(args)
	case "update":
		cmdUpdate(args)
	case "whitelist":
		cmdWhitelist(args)
	case "dns-on":
		cmdDNSOn(args)
	case "dns-off":
		cmdDNSOff(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: adsink <command> [flags]

Commands:
  run         Start the DNS server
  update      Download/refresh blocklists
  whitelist   Manage the domain whitelist

  dns-on      Point system DNS at the local server (requires sudo)
  dns-off     Restore original system DNS           (requires sudo)

Run flags:
  -listen     DNS listen address          (default: 127.0.0.1:53)
  -upstream   Upstream DNS resolver       (default: 8.8.8.8:53)
  -web        Web dashboard address       (default: 127.0.0.1:8080)
  -no-web     Disable the web dashboard
  -data       Data directory              (default: /var/lib/adsink)
  -ttl        Blocklist cache TTL hours   (default: 24)

Whitelist sub-commands:
  adsink whitelist add <domain>
  adsink whitelist remove <domain>
  adsink whitelist list`)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	listen   := fs.String("listen",   defaultListen,   "DNS listen address")
	upstream := fs.String("upstream", defaultUpstream, "upstream DNS resolver")
	webAddr  := fs.String("web",      defaultWebAddr,  "web dashboard address")
	noWeb    := fs.Bool("no-web",     false,           "disable web dashboard")
	data     := fs.String("data",     dataDir(),       "data directory")
	ttlH     := fs.Int("ttl",         24,              "blocklist cache TTL in hours")
	_ = fs.Parse(args)

	bl := blocklist.New(*data, nil, time.Duration(*ttlH)*time.Hour)

	fmt.Println("Loading blocklist...")
	count, err := bl.Load()
	if err != nil {
		log.Fatalf("Failed to load blocklist: %v", err)
	}
	fmt.Printf("Blocklist ready: %d domains\n", count)

	rec := stats.New(count)

	dnsServer := dns.New(bl, rec, *listen, *upstream)
	fmt.Printf("Starting DNS server on %s (upstream: %s)\n", *listen, *upstream)
	if err := dnsServer.Start(); err != nil {
		log.Fatalf("Failed to start DNS server: %v\n\nTip: port 53 requires root or cap_net_bind_service.\nRun with sudo, or use -listen 127.0.0.1:5353 and an iptables redirect.", err)
	}

	if !*noWeb {
		webServer := web.New(rec, *webAddr)
		webServer.Start()
		defer webServer.Stop()
	}

	fmt.Println("Running. Press Ctrl+C to stop.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nShutting down...")
	snap := rec.Snapshot()
	fmt.Printf("Session stats — blocked: %d  allowed: %d  errors: %d  uptime: %s\n",
		snap.TotalBlocked, snap.TotalAllowed, snap.TotalErrors, snap.Uptime)
	dnsServer.Stop()
}

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	data := fs.String("data", dataDir(), "data directory")
	_ = fs.Parse(args)

	bl := blocklist.New(*data, nil, 0)
	fmt.Println("Updating blocklists...")
	count, err := bl.Update()
	if err != nil {
		log.Fatalf("Update failed: %v", err)
	}
	fmt.Printf("Done. %d domains in blocklist.\n", count)
}

func cmdWhitelist(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: adsink whitelist <add|remove|list> [domain]")
		os.Exit(1)
	}
	bl := blocklist.New(dataDir(), nil, 0)
	switch args[0] {
	case "add":
		if len(args) < 2 {
			log.Fatal("Usage: adsink whitelist add <domain>")
		}
		if err := bl.WhitelistAdd(args[1]); err != nil {
			log.Fatalf("whitelist add: %v", err)
		}
		fmt.Printf("Whitelisted: %s\n", args[1])
	case "remove":
		if len(args) < 2 {
			log.Fatal("Usage: adsink whitelist remove <domain>")
		}
		if err := bl.WhitelistRemove(args[1]); err != nil {
			log.Fatalf("whitelist remove: %v", err)
		}
		fmt.Printf("Removed from whitelist: %s\n", args[1])
	case "list":
		entries := bl.WhitelistList()
		if len(entries) == 0 {
			fmt.Println("Whitelist is empty.")
			return
		}
		for _, d := range entries {
			fmt.Println(d)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdDNSOn(args []string) {
	fs := flag.NewFlagSet("dns-on", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1", "local server address")
	_ = fs.Parse(args)
	if err := sysconfig.Apply(*addr); err != nil {
		log.Fatalf("dns-on: %v\n\nTip: run with sudo.", err)
	}
	fmt.Println("System DNS updated.")
}

func cmdDNSOff(_ []string) {
	if err := sysconfig.Remove(); err != nil {
		log.Fatalf("dns-off: %v\n\nTip: run with sudo.", err)
	}
	fmt.Println("System DNS restored.")
}
