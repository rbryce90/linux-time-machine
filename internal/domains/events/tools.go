package events

import (
	"context"
	"fmt"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

type eventsSearch struct{ store Store }

func (t *eventsSearch) Name() string { return "events_search" }
func (t *eventsSearch) Description() string {
	return "Search systemd journal events by substring (case-insensitive over message + unit)."
}
func (t *eventsSearch) Schema() map[string]any {
	return mcp.ObjectSchema(map[string]any{
		"query":             mcp.StringProp("Substring to match against message and unit."),
		"limit":             mcp.IntProp("Max rows to return (default 20)."),
		"since_seconds_ago": mcp.IntProp("Only consider events within this many seconds (default 3600)."),
	}, []string{"query"})
}
func (t *eventsSearch) Invoke(_ context.Context, args map[string]any) (any, error) {
	query := mcp.StringArg(args, "query", "")
	if query == "" {
		return nil, mcp.ErrArg("query is required")
	}
	limit := mcp.IntArg(args, "limit", 20)
	sinceSec := mcp.IntArg(args, "since_seconds_ago", 3600)
	since := time.Now().Add(-time.Duration(sinceSec) * time.Second)

	events, err := t.store.Search(query, limit, since)
	if err != nil {
		return nil, err
	}
	return map[string]any{"query": query, "count": len(events), "events": eventsJSON(events)}, nil
}

type eventsLatest struct{ store Store }

func (t *eventsLatest) Name() string { return "events_latest" }
func (t *eventsLatest) Description() string {
	return "Return the most recent systemd journal events."
}
func (t *eventsLatest) Schema() map[string]any {
	return mcp.ObjectSchema(map[string]any{
		"limit": mcp.IntProp("Max rows to return (default 20)."),
	}, nil)
}
func (t *eventsLatest) Invoke(_ context.Context, args map[string]any) (any, error) {
	limit := mcp.IntArg(args, "limit", 20)
	events, err := t.store.Latest(limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(events), "events": eventsJSON(events)}, nil
}

type eventsNear struct{ store Store }

func (t *eventsNear) Name() string { return "events_near_time" }
func (t *eventsNear) Description() string {
	return "Return journal events within a window around a specific past time."
}
func (t *eventsNear) Schema() map[string]any {
	return mcp.ObjectSchema(map[string]any{
		"seconds_ago":    mcp.IntProp("How far back the anchor time is from now."),
		"window_seconds": mcp.IntProp("Half-width of the window around the anchor (default 60)."),
		"limit":          mcp.IntProp("Max rows to return (default 50)."),
	}, []string{"seconds_ago"})
}
func (t *eventsNear) Invoke(_ context.Context, args map[string]any) (any, error) {
	secondsAgo := mcp.IntArg(args, "seconds_ago", 0)
	if secondsAgo <= 0 {
		return nil, mcp.ErrArg("seconds_ago must be > 0")
	}
	windowSec := mcp.IntArg(args, "window_seconds", 60)
	limit := mcp.IntArg(args, "limit", 50)

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
	return "Semantic search over systemd journal events using local embeddings. Finds entries by meaning, not just keyword."
}
func (t *semanticSearch) Schema() map[string]any {
	return mcp.ObjectSchema(map[string]any{
		"query":             mcp.StringProp("Free-form text describing what to find."),
		"limit":             mcp.IntProp("Max rows to return (default 10)."),
		"since_seconds_ago": mcp.IntProp("Only consider events within this many seconds (default 86400)."),
	}, []string{"query"})
}
func (t *semanticSearch) Invoke(ctx context.Context, args map[string]any) (any, error) {
	if t.ollama == nil {
		return nil, fmt.Errorf("ollama client unavailable; semantic search disabled")
	}
	query := mcp.StringArg(args, "query", "")
	if query == "" {
		return nil, mcp.ErrArg("query is required")
	}
	limit := mcp.IntArg(args, "limit", 10)
	sinceSec := mcp.IntArg(args, "since_seconds_ago", 86400)
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
	return map[string]any{"query": query, "count": len(events), "events": eventsJSON(events)}, nil
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
