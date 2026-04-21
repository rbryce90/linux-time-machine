package system

import (
	"context"

	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

// MCP tools this domain exposes. Each is a small struct implementing mcp.Tool.
// Add one here, register in domain.go's Tools() method — done.

type getCurrentMetrics struct{ store Store }

func (t *getCurrentMetrics) Name() string        { return "get_current_metrics" }
func (t *getCurrentMetrics) Description() string { return "Return the most recent system metrics sample." }
func (t *getCurrentMetrics) Invoke(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"status": "not implemented"}, nil
}

type topProcesses struct{ store Store }

func (t *topProcesses) Name() string        { return "top_processes" }
func (t *topProcesses) Description() string { return "Return the top N processes by CPU or memory within a time range." }
func (t *topProcesses) Invoke(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"status": "not implemented"}, nil
}

func (d *Domain) Tools() []mcp.Tool {
	return []mcp.Tool{
		&getCurrentMetrics{store: d.store},
		&topProcesses{store: d.store},
	}
}
