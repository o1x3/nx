package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/o1x3/nx/internal/token/core"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var ansiX = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func dw(s string) int { return lipgloss.Width(ansiX.ReplaceAllString(s, "")) }

// checkUniform renders every tab under both colour profiles and asserts the
// card never panics and never exceeds the width budget. Lines are intentionally
// ragged now (no background ⇒ no need for uniform width).
func checkUniform(t *testing.T, name string, s core.Summary) {
	for _, prof := range []termenv.Profile{termenv.TrueColor, termenv.Ascii} {
		lipgloss.SetColorProfile(prof)
		for _, tab := range TabOrder {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC %s prof=%v tab=%s: %v", name, prof, tab, r)
					}
				}()
				for i, l := range strings.Split(RenderCard(s, tab), "\n") {
					if w := dw(l); w > 80 {
						t.Errorf("OVER80 %s prof=%v tab=%s line %d w=%d: %q", name, prof, tab, i, w, l)
					}
				}
			}()
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func baseHeatmap(now time.Time) core.Heatmap {
	const weeks = 22
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	first := weekStart.AddDate(0, 0, -7*(weeks-1))
	h := core.Heatmap{Weeks: weeks, FirstDay: first}
	for r := 0; r < 7; r++ {
		h.Cells[r] = make([]int64, weeks)
	}
	return h
}

func TestAdvEmpty(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	s := core.Summary{
		Harness: core.Combined, Range: core.RangeAll,
		PeakHour: -1,
		Heatmap:  baseHeatmap(now),
	}
	checkUniform(t, "empty", s)
}

func TestAdvHuge(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	h := baseHeatmap(now)
	h.Max = 999_000_000_000_000
	h.Cells[0][0] = 999_000_000_000_000
	s := core.Summary{
		Harness: core.Combined, Range: core.RangeAll,
		Sessions: 2_000_000_000, Messages: 2_000_000_000,
		TotalTokens: 999_000_000_000_000,
		ActiveDays:  999_999_999, CurrentStreak: 999999, LongestStreak: 999999,
		PeakHour: 23,
		FavModel: "claude-opus-4-8-super-long-model-name-that-overflows-everything-xxxxxxx",
		Models: []core.ModelStat{
			{Name: "claude-opus-4-8-super-long-model-name-that-overflows", Tokens: 999_000_000_000_000},
			{Name: "x", Tokens: 1},
		},
		Heatmap:      h,
		HobbitFactor: 9_999_999_999,
	}
	checkUniform(t, "huge", s)
}

func TestAdvZeroHobbit(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	s := core.Summary{
		Harness: core.Claude, Range: core.Range7d,
		PeakHour: 0, HobbitFactor: 0,
		Heatmap: baseHeatmap(now),
	}
	checkUniform(t, "zerohobbit", s)
}

func TestAdvLongFav(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	s := core.Summary{
		Harness: core.Pi, Range: core.Range30d,
		FavModel: strings.Repeat("z", 500),
		PeakHour: 12, HobbitFactor: 0.5,
		Heatmap: baseHeatmap(now),
	}
	checkUniform(t, "longfav", s)
}

// FirstDay positioned so a month label lands at the far right edge.
func TestAdvMonthEdge(t *testing.T) {
	// Try many FirstDay values to land a month-start at x near innerWidth.
	for d := 0; d < 400; d++ {
		first := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local).AddDate(0, 0, d)
		h := core.Heatmap{Weeks: 22, FirstDay: first}
		for r := 0; r < 7; r++ {
			h.Cells[r] = make([]int64, 22)
		}
		s := core.Summary{Harness: core.Combined, Range: core.RangeAll, PeakHour: -1, Heatmap: h}
		checkUniform(t, "monthedge_d"+string(rune('0'+d%10)), s)
	}
}

// Weeks larger than expected (renderer trusts h.Weeks; cells sized to match).
func TestAdvManyWeeks(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	for _, weeks := range []int{1, 23, 24, 30, 40, 100} {
		first := weekStart.AddDate(0, 0, -7*(weeks-1))
		h := core.Heatmap{Weeks: weeks, FirstDay: first}
		for r := 0; r < 7; r++ {
			h.Cells[r] = make([]int64, weeks)
		}
		s := core.Summary{Harness: core.Combined, Range: core.RangeAll, PeakHour: -1, Heatmap: h}
		checkUniform(t, "weeks", s)
	}
}

// Cursor summaries carry estimated (bytes/4) token figures; the ≈-marked
// banner and the extra disclaimer lines must stay inside the width budget on
// every tab under both colour profiles.
func TestAdvCursorEstimated(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	h := baseHeatmap(now)
	h.Cells[2][5] = 900_000_000_000
	h.Max = 900_000_000_000
	s := core.Summary{
		Harness: core.Cursor, Range: core.RangeAll,
		Sessions: 12, Messages: 340,
		TotalTokens: 900_000_000_000, InputTokens: 500_000_000_000,
		OutputTokens: 400_000_000_000,
		ActiveDays:   9, CurrentStreak: 2, LongestStreak: 4, PeakHour: 15,
		FavModel: "gpt-5.4", HobbitFactor: 3.2,
		TokensEstimated: true,
		Heatmap:         h,
	}
	checkUniform(t, "cursor", s)
}

func TestAdvSingleActiveDay(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local)
	h := baseHeatmap(now)
	h.Cells[3][10] = 5
	h.Max = 5
	s := core.Summary{
		Harness: core.Codex, Range: core.RangeAll,
		Messages: 1, TotalTokens: 5, ActiveDays: 1,
		CurrentStreak: 1, LongestStreak: 1, PeakHour: 3,
		FavModel: "x", HobbitFactor: 0.00004,
		Heatmap: h,
	}
	checkUniform(t, "single", s)
}
