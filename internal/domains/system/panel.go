package system

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

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
	history []float64 // live-mode rolling window of CPU percentages
	top     []ProcessSample
	cursor  *time.Time // nil -> live; non-nil -> history at this time
}

const sparklineLen = 60

func (p *panel) Title() string { return "System" }

func (p *panel) Refresh() {
	p.mu.Lock()
	if p.cursor != nil {
		// history mode: no-op refresh. View() re-queries on each render
		// from the cursor, so live ticks don't need to mutate state.
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

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

func (p *panel) SetCursor(at *time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cursor = at
}

func (p *panel) View() string {
	p.mu.RLock()
	cursor := p.cursor
	p.mu.RUnlock()

	if cursor != nil {
		return p.viewAt(*cursor)
	}
	return p.viewLive()
}

func (p *panel) viewLive() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.err != nil {
		return theme.Bad.Render(fmt.Sprintf("error: %v", p.err))
	}
	if !p.hasRow {
		return theme.Dim.Render("waiting for first sample…")
	}

	rate := rateSet{}
	if p.hasPrev {
		if elapsed := p.latest.At.Sub(p.prev.At).Seconds(); elapsed > 0 {
			rate = rateSet{
				diskRead:  perSec(p.latest.DiskRead, p.prev.DiskRead, elapsed),
				diskWrite: perSec(p.latest.DiskWrite, p.prev.DiskWrite, elapsed),
				netRx:     perSec(p.latest.NetRx, p.prev.NetRx, elapsed),
				netTx:     perSec(p.latest.NetTx, p.prev.NetTx, elapsed),
			}
		}
	}

	return renderBody(p.latest, rate, p.history, -1, p.top)
}

// viewAt renders the panel at a specific historical timestamp.
// Reads samples/processes from the store on demand — no cached history buffer.
func (p *panel) viewAt(at time.Time) string {
	sample, err := p.store.SampleAt(at)
	if err == sql.ErrNoRows {
		return theme.Dim.Render(fmt.Sprintf("no data at %s", at.Format("15:04:05")))
	}
	if err != nil {
		return theme.Bad.Render(fmt.Sprintf("error: %v", err))
	}

	// Compute rate from the sample just before this one.
	rate := rateSet{}
	prevSamples, _ := p.store.SamplesInRange(sample.At.Add(-2*time.Second), sample.At.Add(-time.Nanosecond))
	if len(prevSamples) > 0 {
		prev := prevSamples[len(prevSamples)-1]
		if elapsed := sample.At.Sub(prev.At).Seconds(); elapsed > 0 {
			rate = rateSet{
				diskRead:  perSec(sample.DiskRead, prev.DiskRead, elapsed),
				diskWrite: perSec(sample.DiskWrite, prev.DiskWrite, elapsed),
				netRx:     perSec(sample.NetRx, prev.NetRx, elapsed),
				netTx:     perSec(sample.NetTx, prev.NetTx, elapsed),
			}
		}
	}

	// 60s sparkline ending at the cursor, with the last char marked.
	windowStart := sample.At.Add(-time.Duration(sparklineLen) * time.Second)
	winSamples, _ := p.store.SamplesInRange(windowStart, sample.At)
	history := make([]float64, 0, len(winSamples))
	for _, s := range winSamples {
		history = append(history, s.CPUPct)
	}
	cursorIdx := len(history) - 1

	top, _ := p.store.TopProcessesAt(at, "cpu", 5)

	return renderBody(sample, rate, history, cursorIdx, top)
}

type rateSet struct {
	diskRead, diskWrite, netRx, netTx int64
}

// renderBody is the shared render for both live and history modes. cursorIdx
// marks which sparkline character is "now" (for history). Pass -1 to skip.
func renderBody(s Sample, rate rateSet, history []float64, cursorIdx int, top []ProcessSample) string {
	memPct := 0.0
	if s.MemTotal > 0 {
		memPct = float64(s.MemUsed) / float64(s.MemTotal) * 100
	}

	var b strings.Builder

	b.WriteString(metricRow("CPU",
		theme.ByPercentStyle(s.CPUPct).Render(fmt.Sprintf("%5.1f%%", s.CPUPct)),
		coloredBar(s.CPUPct, 30),
		""))
	b.WriteString("\n")
	b.WriteString(theme.Label.Render("      "))
	b.WriteString(coloredSparkline(history, cursorIdx))
	b.WriteString(theme.Dim.Render(fmt.Sprintf("  last %ds\n", len(history))))

	b.WriteString(metricRow("MEM",
		theme.ByPercentStyle(memPct).Render(fmt.Sprintf("%5.1f%%", memPct)),
		coloredBar(memPct, 30),
		theme.Value.Render(fmt.Sprintf("%s / %s",
			humanBytes(s.MemUsed), humanBytes(s.MemTotal)))))
	b.WriteString("\n")

	b.WriteString(theme.Label.Render("DISK  "))
	b.WriteString(theme.Label.Render("read "))
	b.WriteString(theme.Value.Render(humanBytes(rate.diskRead)))
	b.WriteString(theme.Dim.Render("/s   "))
	b.WriteString(theme.Label.Render("write "))
	b.WriteString(theme.Value.Render(humanBytes(rate.diskWrite)))
	b.WriteString(theme.Dim.Render("/s\n"))

	b.WriteString(theme.Label.Render("NET   "))
	b.WriteString(theme.Label.Render("rx "))
	b.WriteString(theme.Value.Render(humanBytes(rate.netRx)))
	b.WriteString(theme.Dim.Render("/s   "))
	b.WriteString(theme.Label.Render("tx "))
	b.WriteString(theme.Value.Render(humanBytes(rate.netTx)))
	b.WriteString(theme.Dim.Render("/s\n"))

	if len(top) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.TableHeader.Render(fmt.Sprintf("%-6s  %-6s  %-8s  %s",
			"PID", "CPU%", "MEM", "PROCESS")))
		b.WriteString("\n")
		for _, pr := range top {
			name := pr.Name
			if len(name) > 40 {
				name = name[:40]
			}
			pid := lipgloss.NewStyle().Foreground(theme.Cyan).Render(fmt.Sprintf("%-6d", pr.PID))
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

// coloredSparkline renders the series with per-character color. If cursorIdx
// >= 0, that character is underlined in the accent color to show the cursor.
func coloredSparkline(series []float64, cursorIdx int) string {
	if len(series) == 0 {
		return ""
	}
	var b strings.Builder
	for i, v := range series {
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
		if i == cursorIdx {
			style = style.Background(theme.Red).Foreground(theme.Bg).Bold(true)
		}
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
