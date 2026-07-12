// Package ui renders the nx token dashboard with lipgloss. It paints no background
// and draws no border — everything is foreground colour on the terminal's own
// background (neofetch/fastfetch style), with colours that adapt to a light or
// dark terminal.
package ui

import (
	"github.com/o1x3/nx/internal/token/core"

	"github.com/charmbracelet/lipgloss"
)

// adapt is a light/dark colour pair; lipgloss resolves it against the detected
// terminal background at render time.
func adapt(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// Shared text roles. No backgrounds — these are foreground colours chosen to
// stay legible on either a light or a dark terminal.
var (
	label = adapt("#6a6a78", "#8b8b9c") // row labels, weekday/month labels
	value = adapt("#17171f", "#f2f2f7") // numbers, active text (near-fg on both)
	muted = adapt("#9a9aa6", "#565668") // dot leaders, Less/More, hobbit line
)

// Theme is a per-harness accent + 5-step heatmap ramp. Each colour is a
// light/dark pair so a glance tells you the harness on any terminal.
type Theme struct {
	Name   string
	Accent lipgloss.TerminalColor
	Ramp   [5]lipgloss.TerminalColor // [0]=empty .. [4]=hottest
}

// ThemeFor returns the palette for a harness key.
func ThemeFor(harness string) Theme {
	switch harness {
	case core.Claude:
		// warm peach / coral
		return Theme{
			Name:   "claude",
			Accent: adapt("#c2632e", "#f0b48a"),
			Ramp: [5]lipgloss.TerminalColor{
				adapt("#f0e7e0", "#2e2a26"), adapt("#f3d8c2", "#5c4433"),
				adapt("#e6a878", "#9c6b4a"), adapt("#d17d3f", "#d99e76"),
				adapt("#b35a1d", "#ffcea8"),
			},
		}
	case core.Codex:
		// mint / sea green
		return Theme{
			Name:   "codex",
			Accent: adapt("#1f8a5a", "#86e0b3"),
			Ramp: [5]lipgloss.TerminalColor{
				adapt("#e3efe8", "#26302b"), adapt("#c2e6d2", "#2f5040"),
				adapt("#7fc7a0", "#4e8467"), adapt("#3da876", "#7fc2a0"),
				adapt("#1c7f52", "#bdf3d8"),
			},
		}
	case core.Pi:
		// lavender / periwinkle
		return Theme{
			Name:   "pi",
			Accent: adapt("#6b4fc0", "#b9a6ec"),
			Ramp: [5]lipgloss.TerminalColor{
				adapt("#ebe8f3", "#2b2735"), adapt("#d8cdf0", "#473a5e"),
				adapt("#b09ce0", "#6f5a99"), adapt("#8669cf", "#a48fd0"),
				adapt("#5a3eb5", "#dccbff"),
			},
		}
	case core.Cursor:
		// rose / pink
		return Theme{
			Name:   "cursor",
			Accent: adapt("#c0396f", "#f0a3c4"),
			Ramp: [5]lipgloss.TerminalColor{
				adapt("#f2e6ec", "#302629"), adapt("#f3cede", "#55323f"),
				adapt("#e396b7", "#8f4f68"), adapt("#d1618f", "#d087a5"),
				adapt("#b02e63", "#ffc2da"),
			},
		}
	default:
		// pastel blue
		return Theme{
			Name:   "all",
			Accent: adapt("#2f6fd0", "#9cc0f5"),
			Ramp: [5]lipgloss.TerminalColor{
				adapt("#e9ebf0", "#2a2a36"), adapt("#cdddf6", "#34415c"),
				adapt("#93b6ea", "#52719f"), adapt("#5689d6", "#86acde"),
				adapt("#2563b5", "#c1dbff"),
			},
		}
	}
}

// levelIndex maps a token count to a ramp index 0..4 (0 reserved for empty days).
func (t Theme) levelIndex(v, max int64) int {
	if v <= 0 {
		return 0
	}
	if max <= 0 {
		return 2
	}
	// log-ish bucketing so a few huge days don't wash everything out
	ratio := float64(v) / float64(max)
	switch {
	case ratio >= 0.6:
		return 4
	case ratio >= 0.3:
		return 3
	case ratio >= 0.1:
		return 2
	default:
		return 1
	}
}

// level maps a token count to its ramp colour.
func (t Theme) level(v, max int64) lipgloss.TerminalColor { return t.Ramp[t.levelIndex(v, max)] }

// shadeGlyphs renders each ramp level as a 2-cell printable block, used when
// colour is unavailable (piped / ANSI stripped) so the heatmap density still
// reads as character structure.
var shadeGlyphs = [5]string{"  ", "░░", "▒▒", "▓▓", "██"}
