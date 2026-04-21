package events

import (
	"context"
	"fmt"

	"github.com/rbryce90/linux-time-machine/internal/app"
)

type Config struct {
	// Future: filter priorities, include/exclude units, max retention, etc.
}

type Domain struct {
	cfg       Config
	store     Store
	collector *Collector
	cancel    context.CancelFunc
}

func New(cfg Config) *Domain {
	return &Domain{cfg: cfg}
}

func (d *Domain) Name() string { return "events" }

func (d *Domain) Start(ctx context.Context, deps app.Deps) error {
	d.store = NewSQLiteStore(deps.DB)
	if err := d.store.EnsureSchema(); err != nil {
		return fmt.Errorf("events: schema: %w", err)
	}

	d.collector = NewCollector(d.store)

	if err := deps.MCP.RegisterProvider(d); err != nil {
		return fmt.Errorf("events: mcp register: %w", err)
	}
	deps.TUI.AddProvider(d)

	runCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	go d.collector.Run(runCtx)
	return nil
}

func (d *Domain) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}
