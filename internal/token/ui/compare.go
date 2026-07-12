package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/o1x3/nx/internal/token/core"

	"github.com/charmbracelet/lipgloss"
)

// colCell right-aligns a (possibly styled) value into a w-wide column.
func colCell(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d < 0 {
		d = 0
	}
	return strings.Repeat(" ", d) + s
}

// compareColW picks the column width for n side-by-side harness columns so the
// table always fits the 80-col budget: labelW(12) + 4×16 = 76.
func compareColW(n int) int {
	switch {
	case n <= 1:
		return 40
	case n == 2:
		return 30
	case n == 3:
		return 20
	default:
		return 16
	}
}

// RenderCompare lays the per-harness summaries out as side-by-side columns —
// the headline reason to track more than one harness. The card body uses the
// neutral (combined) theme; only each column header is tinted with its own
// harness accent, so a single accent never mis-tints the whole table.
func RenderCompare(sums []core.Summary, rng string) string {
	neutral := ThemeFor(core.Combined)
	if len(sums) == 0 {
		return styled(neutral.Accent).Bold(true).Render("Harness comparison") + "\n" +
			styled(muted).Render("No harness has any recorded usage"+rangeSuffix(rng)+".")
	}

	const labelW = 12
	colW := compareColW(len(sums))

	var grand int64
	for _, s := range sums {
		grand += s.TotalTokens
	}

	title := styled(neutral.Accent).Bold(true).Render("Harness comparison") +
		styled(muted).Render(" · "+rangeLabel(rng))

	header := strings.Repeat(" ", labelW)
	underline := strings.Repeat(" ", labelW)
	for _, s := range sums {
		th := ThemeFor(s.Harness)
		ht := truncate(core.HarnessTitle(s.Harness), colW-2)
		header += colCell(styled(th.Accent).Bold(true).Render(ht), colW)
		underline += colCell(styled(th.Accent).Render(strings.Repeat("─", lipgloss.Width(ht))), colW)
	}

	row := func(name string, val func(core.Summary) string) string {
		line := styled(label).Render(padRight(name, labelW))
		for _, s := range sums {
			line += colCell(styled(value).Render(val(s)), colW)
		}
		return line
	}

	// share row: a 6-cell per-harness mini-bar + pct of the grand total
	shareRow := styled(label).Render(padRight("share", labelW))
	for _, s := range sums {
		shareRow += colCell(shareCell(s, grand), colW)
	}

	rows := []string{
		title, "",
		header, underline,
		row("tokens", func(s core.Summary) string { return core.FormatTokens(s.TotalTokens) }),
		shareRow,
		row("messages", func(s core.Summary) string { return core.FormatInt(s.Messages) }),
		row("sessions", func(s core.Summary) string { return core.FormatInt(s.Sessions) }),
		row("active days", func(s core.Summary) string { return core.FormatInt(s.ActiveDays) }),
		row("streak", func(s core.Summary) string { return fmt.Sprintf("%dd / %dd", s.CurrentStreak, s.LongestStreak) }),
		row("peak hour", func(s core.Summary) string { return core.FormatHour(s.PeakHour) }),
		row("est. spend", func(s core.Summary) string { return core.FormatUSD(s.Cost.Total) }),
		row("fav model", func(s core.Summary) string { return truncate(s.FavModel, colW-2) }),
	}
	return strings.Join(rows, "\n")
}

func shareCell(s core.Summary, grand int64) string {
	th := ThemeFor(s.Harness)
	pct := 0.0
	if grand > 0 {
		pct = float64(s.TotalTokens) / float64(grand) * 100
	}
	const cells = 6
	filled := int(math.Round(pct / 100 * cells))
	if filled > cells {
		filled = cells
	}
	var bar string
	if ascii() {
		bar = strings.Repeat("█", filled) + strings.Repeat("·", cells-filled)
	} else {
		bar = styled(th.Accent).Render(strings.Repeat("█", filled)) + styled(muted).Render(strings.Repeat("·", cells-filled))
	}
	return bar + styled(muted).Render(fmt.Sprintf(" %.0f%%", pct))
}

func rangeSuffix(rng string) string {
	if rng == core.RangeAll {
		return ""
	}
	return " in the " + rangeLabel(rng)
}
