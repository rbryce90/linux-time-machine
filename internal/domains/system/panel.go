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
	err    error
	hasRow bool
}

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
	p.latest = s
	p.hasRow = true
	p.err = nil
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

	var b strings.Builder
	fmt.Fprintf(&b, "CPU   %5.1f%%  %s\n", s.CPUPct, bar(s.CPUPct, 30))
	fmt.Fprintf(&b, "MEM   %5.1f%%  %s  %s / %s\n",
		memPct, bar(memPct, 30),
		humanBytes(s.MemUsed), humanBytes(s.MemTotal))
	fmt.Fprintf(&b, "DISK  read: %s  write: %s\n",
		humanBytes(s.DiskRead), humanBytes(s.DiskWrite))
	fmt.Fprintf(&b, "NET   rx:   %s  tx:    %s\n",
		humanBytes(s.NetRx), humanBytes(s.NetTx))
	fmt.Fprintf(&b, "\nat %s", s.At.Format("15:04:05"))
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
