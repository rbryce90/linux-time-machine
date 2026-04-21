// Package tui is the shared TUI host. Each domain contributes a Panel; this
// package handles the bubbletea program, periodic refresh, and layout.
package tui

type Panel interface {
	Title() string
	Refresh() // called on each tick; panel re-reads its data source
	View() string
}

type PanelProvider interface {
	Panel() Panel
}
