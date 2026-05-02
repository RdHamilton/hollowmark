// Command mtga-daemon watches MTGA Player.log and forwards events to the BFF.
// Configuration is loaded from a JSON file (default: ~/.mtga-companion/daemon.json)
// and can be overridden with environment variables. The BFF URL is never hardcoded.
//
// Environment variables:
//
//	MTGA_DAEMON_BFF_URL     Base URL of the BFF (required if not in config file)
//	MTGA_DAEMON_JWT         Bearer token for BFF authentication
//	MTGA_DAEMON_LOG_PATH    Override MTGA log file path (auto-detected by default)
//	MTGA_DAEMON_ACCOUNT_ID  MTGA account ID to tag events
//
// Flags:
//
//	-config <path>   Path to JSON config file
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ramonehamilton/mtga-daemon/internal/config"
	"github.com/ramonehamilton/mtga-daemon/internal/daemon"
)

func main() {
	defaultCfgPath := defaultConfigPath()
	cfgPath := flag.String("config", defaultCfgPath, "path to JSON config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	svc := daemon.New(cfg)
	log.Printf("[mtga-daemon] starting, bff=%s", cfg.BFFURL)

	if err := svc.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// defaultConfigPath returns ~/.mtga-companion/daemon.json
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "daemon.json"
	}
	return filepath.Join(home, ".mtga-companion", "daemon.json")
}
