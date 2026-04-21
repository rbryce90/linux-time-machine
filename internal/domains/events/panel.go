package events

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rbryce90/linux-time-machine/internal/tui"
	"github.com/rbryce90/linux-time-machine/internal/tui/theme"
)

type panel struct {
	store  Store
	mu     sync.RWMutex
	events []Event
	cursor *time.Time
	width  int
}

func (p *panel) Title() string { return "Events" }

func (p *panel) Refresh() {
	p.mu.Lock()
	cursor := p.cursor
	p.mu.Unlock()

	var (
		events []Event
		err    error
	)
	if cursor != nil {
		events, err = p.store.EventsNear(*cursor, 30*time.Second, 10)
	} else {
		events, err = p.store.Latest(10)
	}
	if err != nil {
		return
	}
	p.mu.Lock()
	p.events = events
	p.mu.Unlock()
}

func (p *panel) SetCursor(at *time.Time) {
	p.mu.Lock()
	p.cursor = at
	p.mu.Unlock()
	p.Refresh()
}

func (p *panel) SetSize(width, _ int) {
	p.mu.Lock()
	p.width = width
	p.mu.Unlock()
}

func (p *panel) View() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.events) == 0 {
		return theme.Dim.Render("no events captured yet…")
	}

	var b strings.Builder
	b.WriteString(theme.TableHeader.Render(
		fmt.Sprintf("%-8s  %-3s  %-20s  MESSAGE", "TIME", "PRI", "UNIT")))
	b.WriteString("\n")

	maxMsg := p.messageWidth()
	// events from EventsNear come in asc order; Latest comes desc. Render
	// newest-first always for consistency.
	for i := len(p.events) - 1; i >= 0; i-- {
		e := p.events[i]
		unit := truncate(e.Unit, 20)
		msg := truncate(e.Message, maxMsg)

		tm := theme.Dim.Render(e.At.Format("15:04:05"))
		pri := styleForPriority(e.Priority).Render(fmt.Sprintf("%3d", e.Priority))
		u := lipgloss.NewStyle().Foreground(theme.Cyan).Render(fmt.Sprintf("%-20s", unit))
		m := theme.Value.Render(msg)

		fmt.Fprintf(&b, "%s  %s  %s  %s\n", tm, pri, u, m)
	}
	return b.String()
}

func (p *panel) messageWidth() int {
	w := p.width
	if w <= 0 {
		w = 80
	}
	// "HH:MM:SS  PRI  UNIT(20)  " = 8 + 2 + 3 + 2 + 20 + 2 = 37 chars
	m := w - 37
	if m < 20 {
		m = 20
	}
	return m
}

// syslog priority levels:
// 0 emerg, 1 alert, 2 crit, 3 err, 4 warn, 5 notice, 6 info, 7 debug
func styleForPriority(pri int) lipgloss.Style {
	switch {
	case pri <= 3: // emerg..err
		return theme.Bad
	case pri == 4: // warn
		return theme.Warn
	case pri == 5: // notice
		return lipgloss.NewStyle().Foreground(theme.Blue)
	case pri == 6: // info
		return theme.Value
	default: // debug and anything unusual
		return theme.Dim
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func (d *Domain) Panel() tui.Panel {
	return &panel{store: d.store}
}
