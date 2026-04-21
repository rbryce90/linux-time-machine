// Package theme holds the Tokyo Night (pink-accent variant) palette used
// across the TUI. Matches the user's alacritty + i3 + neovim configuration.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette — Tokyo Night pink-accent.
var (
	Bg      = lipgloss.Color("#1a1b26")
	BgAlt   = lipgloss.Color("#15161e")
	Surface = lipgloss.Color("#1f2335")
	Fg      = lipgloss.Color("#c0caf5")
	FgDim   = lipgloss.Color("#a9b1d6")
	Comment = lipgloss.Color("#565f89")
	Border  = lipgloss.Color("#3b4261")

	Red         = lipgloss.Color("#f7768e") // primary accent (alacritty cursor, i3 focus)
	RedMuted    = lipgloss.Color("#c46b7a")
	Orange      = lipgloss.Color("#ff9e64")
	Yellow      = lipgloss.Color("#e0af68")
	Green       = lipgloss.Color("#9ece6a")
	Cyan        = lipgloss.Color("#7dcfff")
	Blue        = lipgloss.Color("#7aa2f7")
	Purple      = lipgloss.Color("#bb9af7")
	PurpleMuted = lipgloss.Color("#9d7cd8")
)

// Common styles that any panel can reuse.
var (
	Title = lipgloss.NewStyle().Bold(true).Foreground(Red)

	PanelTitle = lipgloss.NewStyle().Bold(true).Foreground(Purple)

	PanelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(RedMuted).
			Padding(0, 1).
			MarginTop(1)

	Label = lipgloss.NewStyle().Foreground(Comment)
	Value = lipgloss.NewStyle().Foreground(Fg)
	Dim   = lipgloss.NewStyle().Foreground(FgDim)
	Help  = lipgloss.NewStyle().Foreground(Comment).Italic(true).MarginTop(1)

	TableHeader = lipgloss.NewStyle().Bold(true).Foreground(Blue).Underline(true)
	Good        = lipgloss.NewStyle().Foreground(Green)
	Warn        = lipgloss.NewStyle().Foreground(Yellow)
	Bad         = lipgloss.NewStyle().Foreground(Red)
)

// ByPercent returns a color that ramps green → yellow → red as pct rises.
// Used for CPU/mem bars and for per-character coloring of sparklines.
func ByPercent(pct float64) lipgloss.Color {
	switch {
	case pct < 50:
		return Green
	case pct < 80:
		return Yellow
	default:
		return Red
	}
}

// ByPercentStyle wraps ByPercent in a style for quick use.
func ByPercentStyle(pct float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ByPercent(pct))
}
