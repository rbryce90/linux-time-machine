package system

import (
	"context"
	"fmt"
	"time"

	"github.com/rbryce90/changeName/internal/app"
)

type Config struct {
	SampleInterval time.Duration
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

func (d *Domain) Name() string { return "system" }

// Start wires the domain's collector/store, ensures its schema, and kicks
// off the sampling loop on its own goroutine.
func (d *Domain) Start(ctx context.Context, deps app.Deps) error {
	d.store = NewSQLiteStore(deps.DB)
	if err := d.store.EnsureSchema(); err != nil {
		return fmt.Errorf("system: schema: %w", err)
	}

	d.collector = NewCollector(d.store, d.cfg.SampleInterval)

	if err := deps.MCP.RegisterProvider(d); err != nil {
		return fmt.Errorf("system: mcp register: %w", err)
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
