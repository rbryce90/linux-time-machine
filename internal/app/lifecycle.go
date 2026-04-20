package app

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/rbryce90/pulse/internal/mcp"
	"github.com/rbryce90/pulse/internal/storage"
	"github.com/rbryce90/pulse/internal/tui"
)

type App struct {
	Config   Config
	Registry *Registry
	DB       *storage.SQLite
	MCP      *mcp.Server
	TUI      *tui.App
}

func New(cfg Config) (*App, error) {
	db, err := storage.OpenSQLite(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &App{
		Config:   cfg,
		Registry: NewRegistry(),
		DB:       db,
		MCP:      mcp.NewServer(),
		TUI:      tui.NewApp(),
	}, nil
}

// Run starts every registered domain and blocks until the process receives
// SIGINT/SIGTERM, at which point domains are stopped in reverse order.
func (a *App) Run(parent context.Context) error {
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	deps := Deps{DB: a.DB.DB, MCP: a.MCP, TUI: a.TUI}

	if err := a.Registry.StartAll(ctx, deps); err != nil {
		return fmt.Errorf("registry start: %w", err)
	}
	log.Printf("pulse: started domains=%v", a.Registry.Names())

	errCh := make(chan error, 2)
	go func() { errCh <- a.MCP.Start(ctx) }()
	go func() { errCh <- a.TUI.Run(ctx) }()

	<-ctx.Done()
	log.Printf("pulse: shutting down")

	a.Registry.StopAll()
	_ = a.DB.Close()
	return nil
}
