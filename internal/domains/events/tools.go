package events

import (
	"context"
	"fmt"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

// MCP tools this domain exposes.

type eventsSearch struct{ store Store }

func (t *eventsSearch) Name() string { return "events_search" }
func (t *eventsSearch) Description() string {
	return "Search recent systemd journal events by substring (case-insensitive over message + unit). Args: query (string, required), limit (int, default 20), since_seconds_ago (int, default 3600)."
}
func (t *eventsSearch) Invoke(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	limit := intArg(args, "limit", 20)
	sinceSec := intArg(args, "since_seconds_ago", 3600)
	since := time.Now().Add(-time.Duration(sinceSec) * time.Second)

	events, err := t.store.Search(query, limit, since)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"query":   query,
		"count":   len(events),
		"events":  eventsJSON(events),
	}, nil
}

type eventsLatest struct{ store Store }

func (t *eventsLatest) Name() string { return "events_latest" }
func (t *eventsLatest) Description() string {
	return "Return the most recent systemd journal events. Args: limit (int, default 20)."
}
func (t *eventsLatest) Invoke(_ context.Context, args map[string]any) (any, error) {
	limit := intArg(args, "limit", 20)
	events, err := t.store.Latest(limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(events), "events": eventsJSON(events)}, nil
}

type eventsNear struct{ store Store }

func (t *eventsNear) Name() string { return "events_near_time" }
func (t *eventsNear) Description() string {
	return "Return journal events within a window around a specific time. Args: seconds_ago (int, required), window_seconds (int, default 60), limit (int, default 50)."
}
func (t *eventsNear) Invoke(_ context.Context, args map[string]any) (any, error) {
	secondsAgo := intArg(args, "seconds_ago", 0)
	if secondsAgo <= 0 {
		return nil, errArg("seconds_ago must be > 0")
	}
	windowSec := intArg(args, "window_seconds", 60)
	limit := intArg(args, "limit", 50)

	at := time.Now().Add(-time.Duration(secondsAgo) * time.Second)
	events, err := t.store.EventsNear(at, time.Duration(windowSec)*time.Second, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"anchor_at":      at.Format(time.RFC3339),
		"window_seconds": windowSec,
		"count":          len(events),
		"events":         eventsJSON(events),
	}, nil
}

type semanticSearch struct {
	store  Store
	ollama *ollama.Client
}

func (t *semanticSearch) Name() string { return "events_semantic_search" }
func (t *semanticSearch) Description() string {
	return "Semantic search over systemd journal events using local embeddings. Finds entries by meaning, not just keyword. Args: query (string, required), limit (int, default 10), since_seconds_ago (int, default 86400)."
}
func (t *semanticSearch) Invoke(ctx context.Context, args map[string]any) (any, error) {
	if t.ollama == nil {
		return nil, fmt.Errorf("ollama client unavailable; semantic search disabled")
	}
	query, _ := args["query"].(string)
	if query == "" {
		return nil, errArg("query is required")
	}
	limit := intArg(args, "limit", 10)
	sinceSec := intArg(args, "since_seconds_ago", 86400)
	since := time.Now().Add(-time.Duration(sinceSec) * time.Second)

	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	vec, err := t.ollama.Embed(qctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	events, err := t.store.SemanticSearch(vec, limit, since)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"query":  query,
		"count":  len(events),
		"events": eventsJSON(events),
	}, nil
}

func (d *Domain) Tools() []mcp.Tool {
	tools := []mcp.Tool{
		&eventsSearch{store: d.store},
		&eventsLatest{store: d.store},
		&eventsNear{store: d.store},
	}
	if d.ollama != nil {
		tools = append(tools, &semanticSearch{store: d.store, ollama: d.ollama})
	}
	return tools
}

// Helpers

func eventsJSON(events []Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"at":       e.At.Format(time.RFC3339Nano),
			"priority": e.Priority,
			"unit":     e.Unit,
			"source":   e.Source,
			"pid":      e.PID,
			"message":  e.Message,
		})
	}
	return out
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}

type argErr string

func (e argErr) Error() string { return string(e) }
func errArg(msg string) error  { return argErr(msg) }
