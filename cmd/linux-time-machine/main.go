package main

import (
	"context"
	"log"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/app"
	"github.com/rbryce90/linux-time-machine/internal/domains/system"
)

func main() {
	cfg := app.DefaultConfig()

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("pulse: init: %v", err)
	}

	if cfg.Domains.System.Enabled {
		a.Registry.Register(system.New(system.Config{
			SampleInterval: time.Duration(cfg.Domains.System.SampleInterval) * time.Second,
		}))
	}

	// Future: events and network domains register here behind their toggles.

	if err := a.Run(context.Background()); err != nil {
		log.Fatalf("pulse: run: %v", err)
	}
}
