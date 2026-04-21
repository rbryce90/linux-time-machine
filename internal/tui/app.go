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
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.now = time.Time(msg)
		for _, p := range m.panels {
			p.Refresh()
		}
		return m, tick()
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	header := renderHeader(m.name, m.startAt, m.now, m.width)
	b.WriteString(header)
	b.WriteString("\n")

	for _, p := range m.panels {
		body := fmt.Sprintf("%s\n\n%s",
			theme.PanelTitle.Render(p.Title()),
			p.View(),
		)
		b.WriteString(theme.PanelBorder.Render(body))
		b.WriteString("\n")
	}

	b.WriteString(theme.Help.Render("  q / ctrl-c / esc  quit"))
	return b.String()
}

func renderHeader(name string, startAt, now time.Time, width int) string {
	left := theme.Title.Render("▍ " + name)
	uptime := now.Sub(startAt).Round(time.Second)
	right := theme.Dim.Render(fmt.Sprintf("uptime %s  •  %s",
		uptime, now.Format("15:04:05")))

	// lipgloss.PlaceHorizontal handles the spacing; fall back to plain if width is 0.
	if width <= 0 {
		return left + "   " + right
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
