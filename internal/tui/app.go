package tui

import (
	"context"
	"fmt"
	"strings"
)

type App struct {
	panels []Panel
}

func NewApp() *App {
	return &App{}
}

func (a *App) AddPanel(p Panel) {
	a.panels = append(a.panels, p)
}

func (a *App) AddProvider(p PanelProvider) {
	if panel := p.Panel(); panel != nil {
		a.AddPanel(panel)
	}
}

// Run will host the bubbletea program. For scaffolding it just prints the
// registered panels once and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	var b strings.Builder
	b.WriteString("pulse — registered panels:\n")
	for _, p := range a.panels {
		fmt.Fprintf(&b, "  - %s\n", p.Title())
	}
	fmt.Println(b.String())
	<-ctx.Done()
	return nil
}
