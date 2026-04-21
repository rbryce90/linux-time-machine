package system

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/rbryce90/linux-time-machine/internal/tui"
)

type panel struct {
	store  Store
	mu     sync.RWMutex
	latest Sample
	prev   Sample
	hasPrev bool
	err    error
	hasRow bool
	history []float64 // last N CPU percentages for sparkline
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
		return fmt.Sprintf("error: %v", p.err)
	}
	if !p.hasRow {
		return "waiting for first sample…"
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
	fmt.Fprintf(&b, "CPU   %5.1f%%  %s\n", s.CPUPct, bar(s.CPUPct, 30))
	fmt.Fprintf(&b, "            %s  (last %ds)\n", sparkline(p.history), len(p.history))
	fmt.Fprintf(&b, "MEM   %5.1f%%  %s  %s / %s\n",
		memPct, bar(memPct, 30),
		humanBytes(s.MemUsed), humanBytes(s.MemTotal))
	fmt.Fprintf(&b, "DISK  read: %s/s  write: %s/s\n",
		humanBytes(diskReadRate), humanBytes(diskWriteRate))
	fmt.Fprintf(&b, "NET   rx:   %s/s  tx:    %s/s\n",
		humanBytes(netRxRate), humanBytes(netTxRate))
	if len(p.top) > 0 {
		b.WriteString("\nTOP   PID     CPU%    MEM       NAME\n")
		for _, pr := range p.top {
			name := pr.Name
			if len(name) > 32 {
				name = name[:32]
			}
			fmt.Fprintf(&b, "      %-6d  %5.1f   %-8s  %s\n",
				pr.PID, pr.CPUPct, humanBytes(pr.MemRSS), name)
		}
	}

	fmt.Fprintf(&b, "\nat %s", s.At.Format("15:04:05"))
	return b.String()
}

func perSec(cur, prev int64, seconds float64) int64 {
	diff := cur - prev
	if diff < 0 {
		return 0
	}
	return int64(float64(diff) / seconds)
}

var sparkRunes = []rune(" ▁▂▃▄▅▆▇█")

// sparkline renders a series of 0-100 percentages as unicode block chars.
func sparkline(series []float64) string {
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
		b.WriteRune(sparkRunes[idx])
	}
	return b.String()
}

func bar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
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
