package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/app"
	"github.com/rbryce90/linux-time-machine/internal/domains/events"
	"github.com/rbryce90/linux-time-machine/internal/domains/system"
)

func main() {
	mcpMode := flag.Bool("mcp", false, "run as MCP server on stdio (for Claude Desktop)")
	dbPath := flag.String("db", "", "override default SQLite path")
	flag.Parse()

	cfg := app.DefaultConfig()
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if *mcpMode {
		cfg.Mode = app.ModeMCP
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("%s: init: %v", app.Name, err)
	}

	if cfg.Domains.System.Enabled {
		a.Registry.Register(system.New(system.Config{
			SampleInterval: time.Duration(cfg.Domains.System.SampleInterval) * time.Second,
		}))
	}

	if cfg.Domains.Events.Enabled {
		a.Registry.Register(events.New(events.Config{
			RetentionDays: cfg.Domains.Events.RetentionDays,
		}))
	}

	// Future: network domain registers here behind its toggle.

	if err := a.Run(context.Background()); err != nil {
		log.Fatalf("%s: run: %v", app.Name, err)
	}
}
