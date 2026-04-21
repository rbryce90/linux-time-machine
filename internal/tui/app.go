package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type App struct {
	name   string
	panels []Panel
}

func NewApp(name string) *App {
	return &App{name: name}
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
		newModel(a.name, a.panels),
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
	name   string
	panels []Panel
	width  int
	height int
}

func newModel(name string, panels []Panel) model {
	return model{name: name, panels: panels}
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
		for _, p := range m.panels {
			p.Refresh()
		}
		return m, tick()
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7aa2f7")). // tokyo night blue
			MarginBottom(1)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#bb9af7")). // tokyo night purple
			Underline(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#565f89")). // tokyo night comment
			Padding(0, 1).
			MarginTop(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565f89")).
			MarginTop(1)
)

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.name))
	b.WriteString("\n")

	for _, p := range m.panels {
		body := fmt.Sprintf("%s\n\n%s",
			panelTitleStyle.Render(p.Title()),
			p.View(),
		)
		b.WriteString(panelStyle.Render(body))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("press q or ctrl+c to quit"))
	return b.String()
}
