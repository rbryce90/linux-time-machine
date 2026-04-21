package system

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

// MCP tools this domain exposes. Each is a small struct implementing mcp.Tool.
// Add one here, register in domain.go's Tools() method — done.

type getCurrentMetrics struct{ store Store }

func (t *getCurrentMetrics) Name() string { return "system_current_metrics" }
func (t *getCurrentMetrics) Description() string {
	return "Return the most recent system metrics sample (CPU %, memory, disk I/O counters, network bytes)."
}
func (t *getCurrentMetrics) Invoke(_ context.Context, _ map[string]any) (any, error) {
	sample, err := t.store.LatestSample()
	if err != nil {
		if err == sql.ErrNoRows {
			return map[string]any{"status": "no samples yet"}, nil
		}
		return nil, err
	}
	return sampleJSON(sample), nil
}

type getMetricsHistory struct{ store Store }

func (t *getMetricsHistory) Name() string { return "system_metrics_history" }
func (t *getMetricsHistory) Description() string {
	return "Return system metrics samples within a time range. Args: start_seconds_ago (int, default 300), end_seconds_ago (int, default 0)."
}
func (t *getMetricsHistory) Invoke(_ context.Context, args map[string]any) (any, error) {
	start := intArg(args, "start_seconds_ago", 300)
	end := intArg(args, "end_seconds_ago", 0)
	if start <= end {
		return nil, fmt.Errorf("start_seconds_ago must be greater than end_seconds_ago")
	}
	now := time.Now()
	samples, err := t.store.SamplesInRange(
		now.Add(-time.Duration(start)*time.Second),
		now.Add(-time.Duration(end)*time.Second),
	)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(samples))
	for _, s := range samples {
		out = append(out, sampleJSON(s))
	}
	return map[string]any{"count": len(out), "samples": out}, nil
}

func sampleJSON(s Sample) map[string]any {
	return map[string]any{
		"at":         s.At.Format(time.RFC3339Nano),
		"cpu_pct":    s.CPUPct,
		"mem_used":   s.MemUsed,
		"mem_total":  s.MemTotal,
		"disk_read":  s.DiskRead,
		"disk_write": s.DiskWrite,
		"net_rx":     s.NetRx,
		"net_tx":     s.NetTx,
	}
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

type topProcesses struct{ store Store }

func (t *topProcesses) Name() string { return "system_top_processes" }
func (t *topProcesses) Description() string {
	return "Return the processes currently using the most CPU or memory. Args: metric (\"cpu\" or \"mem\", default \"cpu\"), limit (int, default 10)."
}
func (t *topProcesses) Invoke(_ context.Context, args map[string]any) (any, error) {
	metric := "cpu"
	if v, ok := args["metric"].(string); ok && v != "" {
		metric = v
	}
	limit := intArg(args, "limit", 10)
	ps, err := t.store.TopProcessesRecent(metric, limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]any{
			"pid":     p.PID,
			"name":    p.Name,
			"cpu_pct": p.CPUPct,
			"mem_rss": p.MemRSS,
			"at":      p.At.Format(time.RFC3339Nano),
		})
	}
	return map[string]any{"metric": metric, "count": len(out), "processes": out}, nil
}

func (d *Domain) Tools() []mcp.Tool {
	return []mcp.Tool{
		&getCurrentMetrics{store: d.store},
		&getMetricsHistory{store: d.store},
		&topProcesses{store: d.store},
	}
}
