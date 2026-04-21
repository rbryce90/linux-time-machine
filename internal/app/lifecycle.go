package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rbryce90/linux-time-machine/internal/mcp"
	"github.com/rbryce90/linux-time-machine/internal/storage"
	"github.com/rbryce90/linux-time-machine/internal/tui"
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
		MCP:      mcp.NewServer(Name, "v0.0.1"),
		TUI:      tui.NewApp(Name),
	}, nil
}

// Run starts every registered domain, then either serves the MCP protocol
// on stdio (ModeMCP) or runs the TUI (ModeTUI). Blocks until SIGINT/SIGTERM
// or the other side (Claude Desktop / user) disconnects.
func (a *App) Run(parent context.Context) error {
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a.redirectLogs()

	deps := Deps{DB: a.DB.DB, MCP: a.MCP, TUI: a.TUI}
	if err := a.Registry.StartAll(ctx, deps); err != nil {
		return fmt.Errorf("registry start: %w", err)
	}
	log.Printf("%s: started domains=%v mode=%v", Name, a.Registry.Names(), a.Config.Mode)

	defer func() {
		log.Printf("%s: shutting down", Name)
		a.Registry.StopAll()
		_ = a.DB.Close()
	}()

	switch a.Config.Mode {
	case ModeMCP:
		return a.MCP.ServeStdio(ctx)
	default:
		return a.TUI.Run(ctx)
	}
}

// redirectLogs keeps the log stream out of the terminal.
//   - ModeMCP: stdout is the JSON-RPC wire; logs go to stderr.
//   - ModeTUI: bubbletea owns the terminal; logs go to <dir>/<Name>.log or,
//     failing that, are silenced. We never write logs to stdout/stderr in
//     TUI mode because that corrupts the rendered view.
func (a *App) redirectLogs() {
	switch a.Config.Mode {
	case ModeMCP:
		log.SetOutput(os.Stderr)
	default:
		logPath := filepath.Join(filepath.Dir(a.Config.DBPath), Name+".log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.SetOutput(io.Discard)
			return
		}
		log.SetOutput(f)
	}
}
