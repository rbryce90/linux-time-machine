package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
	"github.com/rbryce90/linux-time-machine/internal/agent"
	"github.com/rbryce90/linux-time-machine/internal/app"
	"github.com/rbryce90/linux-time-machine/internal/domains/events"
	"github.com/rbryce90/linux-time-machine/internal/domains/system"
)

func main() {
	dbPath := "/tmp/agent-smoke.db"
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	cfg := app.DefaultConfig()
	cfg.DBPath = dbPath
	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	a.Registry.Register(system.New(system.Config{SampleInterval: time.Second}))
	a.Registry.Register(events.New(events.Config{}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := a.Registry.StartAll(ctx, app.Deps{DB: a.DB.DB, MCP: a.MCP, TUI: a.TUI, Ollama: a.Ollama}); err != nil {
		log.Fatal(err)
	}
	defer a.Registry.StopAll()
	defer a.DB.Close()

	// Let the collector gather a few samples before asking.
	fmt.Println("collecting for 5s...")
	time.Sleep(5 * time.Second)

	tools, invoker := agent.FromMCPTools(a.MCP)
	oll := a.Ollama
	if model := os.Getenv("LTM_CHAT_MODEL"); model != "" {
		oll = ollama.New(ollama.WithChatModel(model))
	}

	ag := &agent.Agent{
		Provider:     oll,
		Tools:        tools,
		Invoker:      invoker,
		MaxTurns:     5,
		SystemPrompt: "You investigate this Linux machine by calling the provided tools. Prefer calling a tool to guessing. Be terse.",
	}

	fmt.Printf("\nusing model: %s\n", oll.Model())
	fmt.Println("tools registered:")
	for _, t := range tools {
		fmt.Printf("  - %s\n", t.Name)
	}

	q := "What are the top 3 processes by CPU right now?"
	if len(os.Args) > 1 {
		q = os.Args[1]
	}
	fmt.Printf("\n> %s\n\n", q)

	answer, err := ag.Run(ctx, q, func(e agent.Event) {
		switch e.Kind {
		case agent.EventTurn:
			fmt.Printf("[%s]\n", e.Content)
		case agent.EventToolCall:
			fmt.Printf("  -> tool: %s args=%v\n", e.ToolName, e.ToolArgs)
		case agent.EventToolResult:
			r := e.ToolResult
			if len(r) > 200 {
				r = r[:200] + "…"
			}
			fmt.Printf("  <- result: %s\n", r)
		case agent.EventError:
			fmt.Printf("  ! error: %v (tool=%s)\n", e.Err, e.ToolName)
		}
	})
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	fmt.Printf("\nanswer:\n%s\n", answer)
}
