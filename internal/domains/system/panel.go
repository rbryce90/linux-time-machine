package system

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/rbryce90/linux-time-machine/internal/tui"
	"github.com/rbryce90/linux-time-machine/internal/tui/theme"
)

type panel struct {
	store   Store
	mu      sync.RWMutex
	latest  Sample
	prev    Sample
	hasPrev bool
	err     error
	hasRow  bool
	history []float64
	top     []ProcessSample
}

const sparklineLen = 60

func (p *panel) Title() string { return "System" }

func (p *panel) Refresh() {
	s, err := p.store.LatestSample()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if err == sql.ErrNoRows {
			p.hasRow = false
			p.err = nil
			return
		}
		p.err = err
		return
	}
	if p.hasRow {
		p.prev = p.latest
		p.hasPrev = true
	}
	p.latest = s
	p.hasRow = true
	p.err = nil

	p.history = append(p.history, s.CPUPct)
	if len(p.history) > sparklineLen {
		p.history = p.history[len(p.history)-sparklineLen:]
	}

	if top, err := p.store.TopProcessesRecent("cpu", 5); err == nil {
		p.top = top
	}
}

func (p *panel) View() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.err != nil {
		return theme.Bad.Render(fmt.Sprintf("error: %v", p.err))
	}
	if !p.hasRow {
		return theme.Dim.Render("waiting for first sample…")
	}

	s := p.latest
	memPct := 0.0
	if s.MemTotal > 0 {
		memPct = float64(s.MemUsed) / float64(s.MemTotal) * 100
	}

	diskReadRate, diskWriteRate, netRxRate, netTxRate := int64(0), int64(0), int64(0), int64(0)
	if p.hasPrev {
		elapsed := s.At.Sub(p.prev.At).Seconds()
		if elapsed > 0 {
			diskReadRate = perSec(s.DiskRead, p.prev.DiskRead, elapsed)
			diskWriteRate = perSec(s.DiskWrite, p.prev.DiskWrite, elapsed)
			netRxRate = perSec(s.NetRx, p.prev.NetRx, elapsed)
			netTxRate = perSec(s.NetTx, p.prev.NetTx, elapsed)
		}
	}

	var b strings.Builder

	// CPU row
	b.WriteString(metricRow("CPU",
		theme.ByPercentStyle(s.CPUPct).Render(fmt.Sprintf("%5.1f%%", s.CPUPct)),
		coloredBar(s.CPUPct, 30),
		""))
	b.WriteString("\n")
	// Sparkline on its own line, indented under CPU
	b.WriteString(theme.Label.Render("      "))
	b.WriteString(coloredSparkline(p.history))
	b.WriteString(theme.Dim.Render(fmt.Sprintf("  last %ds\n", len(p.history))))

	// MEM row
	b.WriteString(metricRow("MEM",
		theme.ByPercentStyle(memPct).Render(fmt.Sprintf("%5.1f%%", memPct)),
		coloredBar(memPct, 30),
		theme.Value.Render(fmt.Sprintf("%s / %s",
			humanBytes(s.MemUsed), humanBytes(s.MemTotal)))))
	b.WriteString("\n")

	// DISK row
	b.WriteString(theme.Label.Render("DISK  "))
	b.WriteString(theme.Label.Render("read "))
	b.WriteString(theme.Value.Render(humanBytes(diskReadRate)))
	b.WriteString(theme.Dim.Render("/s   "))
	b.WriteString(theme.Label.Render("write "))
	b.WriteString(theme.Value.Render(humanBytes(diskWriteRate)))
	b.WriteString(theme.Dim.Render("/s"))
	b.WriteString("\n")

	// NET row
	b.WriteString(theme.Label.Render("NET   "))
	b.WriteString(theme.Label.Render("rx "))
	b.WriteString(theme.Value.Render(humanBytes(netRxRate)))
	b.WriteString(theme.Dim.Render("/s   "))
	b.WriteString(theme.Label.Render("tx "))
	b.WriteString(theme.Value.Render(humanBytes(netTxRate)))
	b.WriteString(theme.Dim.Render("/s"))
	b.WriteString("\n")

	if len(p.top) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.TableHeader.Render(fmt.Sprintf("%-6s  %-6s  %-8s  %s",
			"PID", "CPU%", "MEM", "PROCESS")))
		b.WriteString("\n")
		for _, pr := range p.top {
			name := pr.Name
			if len(name) > 40 {
				name = name[:40]
			}
			pid := theme.ByPercentStyle(0).Copy().Foreground(theme.Cyan).
				Render(fmt.Sprintf("%-6d", pr.PID))
			cpu := theme.ByPercentStyle(pr.CPUPct).Render(fmt.Sprintf("%5.1f ", pr.CPUPct))
			mem := theme.Warn.Render(fmt.Sprintf("%-8s", humanBytes(pr.MemRSS)))
			nm := theme.Value.Render(name)
			fmt.Fprintf(&b, "%s  %s  %s  %s\n", pid, cpu, mem, nm)
		}
	}

	b.WriteString("\n")
	b.WriteString(theme.Dim.Render("at " + s.At.Format("15:04:05")))
	return b.String()
}

// metricRow renders: "LABEL  VALUE  BAR  EXTRA".
func metricRow(label, value, bar, extra string) string {
	out := theme.Label.Render(fmt.Sprintf("%-4s  ", label)) + value + "  " + bar
	if extra != "" {
		out += "  " + extra
	}
	return out
}

func perSec(cur, prev int64, seconds float64) int64 {
	diff := cur - prev
	if diff < 0 {
		return 0
	}
	return int64(float64(diff) / seconds)
}

// coloredBar renders a percentage bar in a color that reflects its value.
func coloredBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	return theme.ByPercentStyle(pct).Render(strings.Repeat("█", filled)) +
		theme.Label.Render(strings.Repeat("░", width-filled))
}

var sparkRunes = []rune(" ▁▂▃▄▅▆▇█")

// coloredSparkline renders per-character colors based on each sample's value.
func coloredSparkline(series []float64) string {
	if len(series) == 0 {
		return ""
	}
	var b strings.Builder
	for _, v := range series {
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		idx := int(v / 100 * float64(len(sparkRunes)-1))
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		style := lipgloss.NewStyle().Foreground(theme.ByPercent(v))
		b.WriteString(style.Render(string(sparkRunes[idx])))
	}
	return b.String()
}

func humanBytes(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case n >= TB:
		return fmt.Sprintf("%.2f TB", float64(n)/TB)
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/GB)
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/MB)
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (d *Domain) Panel() tui.Panel {
	return &panel{store: d.store}
}
