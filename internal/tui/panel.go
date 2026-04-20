// Package tui is the shared TUI host. Each domain contributes a Panel; this
// package knows nothing about any specific domain. bubbletea/lipgloss will
// be introduced when we wire the real renderer.
package tui

type Panel interface {
	Title() string
	View() string
}

type PanelProvider interface {
	Panel() Panel
}
