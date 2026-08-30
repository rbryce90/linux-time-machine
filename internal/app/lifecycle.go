package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
	"github.com/rbryce90/linux-time-machine/internal/agent"
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
	Ollama   *ollama.Client
}

func New(cfg Config) (*App, error) {
	db, err := storage.OpenSQLite(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	oll := ollama.New()
	return &App{
		Config:   cfg,
		Registry: NewRegistry(),
		DB:       db,
		MCP:      mcp.NewServer(Name, "v0.0.1"),
		TUI:      tui.NewApp(Name),
		Ollama:   oll,
	}, nil
}

// Run starts every registered domain, then either serves the MCP protocol
// on stdio (ModeMCP) or runs the TUI (ModeTUI). Blocks until SIGINT/SIGTERM
// or the other side (Claude Desktop / user) disconnects.
func (a *App) Run(parent context.Context) error {
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a.redirectLogs()

	// Ping Ollama; if it's unreachable we still pass the client along
	// (domains can feature-detect), but note it in the log.
	pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
	ollamaErr := a.Ollama.Ping(pingCtx)
	pingCancel()
	if ollamaErr != nil {
		log.Printf("%s: ollama unreachable (%v) — semantic features will be disabled", Name, ollamaErr)
	} else {
		log.Printf("%s: ollama ready, embedding model=%s", Name, a.Ollama.EmbeddingModel())
	}

	deps := Deps{DB: a.DB.DB, MCP: a.MCP, TUI: a.TUI, Ollama: a.Ollama}
	if err := a.Registry.StartAll(ctx, deps); err != nil {
		return fmt.Errorf("registry start: %w", err)
	}
	log.Printf("%s: started domains=%v mode=%v", Name, a.Registry.Names(), a.Config.Mode)

	// Wire the chat agent only in TUI mode — MCP mode has no human UI.
	// The agent must be built AFTER domains register their MCP tools.
	if a.Config.Mode == ModeTUI {
		a.wireChatAgent(ollamaErr)
	}

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

// wireChatAgent builds the tool-calling agent from the registered MCP tools
// and hands it to the TUI. If the startup Ollama ping failed we pass a
// disabled reason so the chat panel renders a useful message on first open
// rather than crashing when the user hits `c`.
func (a *App) wireChatAgent(ollamaErr error) {
	if ollamaErr != nil {
		a.TUI.SetAgent(nil, fmt.Sprintf("Ollama is not reachable at %s — start `ollama serve` and restart this app",
			strings.TrimPrefix(ollama.DefaultBaseURL, "http://")))
		return
	}
	tools, invoker := agent.FromMCPTools(a.MCP)
	a.TUI.SetAgent(&agent.Agent{
		Provider: a.Ollama,
		Tools:    tools,
		Invoker:  invoker,
		MaxTurns: 6,
	}, "")
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
