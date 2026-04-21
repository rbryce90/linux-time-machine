// Package tui is the shared TUI host. Each domain contributes a Panel; this
// package handles the bubbletea program, periodic refresh, and layout.
package tui

import "time"

type Panel interface {
	Title() string
	Refresh()                 // called on each live-mode tick
	SetCursor(at *time.Time)  // nil = live mode; non-nil = historical view at this time
	View() string
}

type PanelProvider interface {
	Panel() Panel
}
