package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rbryce90/linux-time-machine/internal/tui/theme"
)

type App struct {
	name    string
	panels  []Panel
	startAt time.Time
}

func NewApp(name string) *App {
	return &App{name: name, startAt: time.Now()}
}

func (a *App) AddPanel(p Panel) {
	a.panels = append(a.panels, p)
}

func (a *App) AddProvider(p PanelProvider) {
	if panel := p.Panel(); panel != nil {
		a.AddPanel(panel)
	}
}

func (a *App) Run(ctx context.Context) error {
	p := tea.NewProgram(
		newModel(a.name, a.startAt, a.panels),
		tea.WithContext(ctx),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}

type viewMode int

const (
	modeLive viewMode = iota
	modeHistory
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type model struct {
	name    string
	startAt time.Time
	panels  []Panel
	width   int
	height  int
	now     time.Time

	mode   viewMode
	cursor time.Time
}

func newModel(name string, startAt time.Time, panels []Panel) model {
	return model{name: name, startAt: startAt, panels: panels, now: time.Now()}
}

func (m model) Init() tea.Cmd {
	for _, p := range m.panels {
		p.Refresh()
	}
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Account for the panel border (2 chars) and padding (2 chars).
		innerWidth := msg.Width - 4
		if innerWidth < 10 {
			innerWidth = 10
		}
		for _, p := range m.panels {
			p.SetSize(innerWidth, msg.Height)
		}
	case tickMsg:
		m.now = time.Time(msg)
		if m.mode == modeLive {
			for _, p := range m.panels {
				p.Refresh()
			}
		}
		return m, tick()
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.mode == modeHistory {
			m.setLive()
			return m, nil
		}
		return m, tea.Quit

	case "h":
		if m.mode == modeLive {
			m.setHistory(m.now.Add(-1 * time.Second))
		}
		return m, nil

	case "left":
		if m.mode == modeHistory {
			m.setHistory(m.cursor.Add(-1 * time.Second))
		}
		return m, nil
	case "right":
		if m.mode == modeHistory {
			next := m.cursor.Add(1 * time.Second)
			if next.After(m.now) {
				m.setLive()
			} else {
				m.setHistory(next)
			}
		}
		return m, nil
	case "shift+left":
		if m.mode == modeHistory {
			m.setHistory(m.cursor.Add(-10 * time.Second))
		}
		return m, nil
	case "shift+right":
		if m.mode == modeHistory {
			next := m.cursor.Add(10 * time.Second)
			if next.After(m.now) {
				m.setLive()
			} else {
				m.setHistory(next)
			}
		}
		return m, nil
	case "home":
		if m.mode == modeHistory {
			m.setHistory(m.cursor.Add(-60 * time.Second))
		}
		return m, nil
	case "end":
		if m.mode == modeHistory {
			m.setLive()
		}
		return m, nil
	}
	return m, nil
}

func (m *model) setLive() {
	m.mode = modeLive
	for _, p := range m.panels {
		p.SetCursor(nil)
		p.Refresh()
	}
}

func (m *model) setHistory(at time.Time) {
	m.mode = modeHistory
	m.cursor = at
	for _, p := range m.panels {
		p.SetCursor(&at)
	}
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(renderHeader(m.name, m.startAt, m.now, m.mode, m.cursor, m.width))
	b.WriteString("\n")

	for _, p := range m.panels {
		body := fmt.Sprintf("%s\n\n%s",
			theme.PanelTitle.Render(p.Title()),
			p.View(),
		)
		b.WriteString(theme.PanelBorder.Render(body))
		b.WriteString("\n")
	}

	b.WriteString(theme.Help.Render(helpText(m.mode)))
	return b.String()
}

func helpText(m viewMode) string {
	if m == modeHistory {
		return "  ← →  scrub 1s    shift+← →  scrub 10s    home  back 1m    end / esc  live    q  quit"
	}
	return "  h  history mode    q / ctrl-c / esc  quit"
}

func renderHeader(name string, startAt, now time.Time, mode viewMode, cursor time.Time, width int) string {
	left := theme.Title.Render("▍ " + name)

	var middle string
	switch mode {
	case modeHistory:
		delta := now.Sub(cursor).Round(time.Second)
		middle = lipgloss.NewStyle().
			Foreground(theme.Bg).Background(theme.Red).Bold(true).Padding(0, 1).
			Render(fmt.Sprintf("⏴ HIST  %s  (-%s)", cursor.Format("15:04:05"), delta))
	default:
		middle = lipgloss.NewStyle().
			Foreground(theme.Bg).Background(theme.Green).Bold(true).Padding(0, 1).
			Render("● LIVE")
	}

	uptime := now.Sub(startAt).Round(time.Second)
	right := theme.Dim.Render(fmt.Sprintf("uptime %s  •  %s", uptime, now.Format("15:04:05")))

	if width <= 0 {
		return left + "   " + middle + "   " + right
	}

	gap1 := (width - lipgloss.Width(left) - lipgloss.Width(middle) - lipgloss.Width(right)) / 2
	gap2 := width - lipgloss.Width(left) - lipgloss.Width(middle) - lipgloss.Width(right) - gap1
	if gap1 < 1 {
		gap1 = 1
	}
	if gap2 < 1 {
		gap2 = 1
	}
	return left + strings.Repeat(" ", gap1) + middle + strings.Repeat(" ", gap2) + right
}
