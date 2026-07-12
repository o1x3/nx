package core

import (
	"testing"
	"time"
)

func TestFriendlyModel(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8":           "Opus 4.8",
		"claude-opus-4-8[1m]":       "Opus 4.8",
		"claude-sonnet-4-6":         "Sonnet 4.6",
		"claude-haiku-4-5-20251001": "Haiku 4.5",
		"claude-fable-5":            "Fable 5",
		"gpt-5.4":                   "GPT-5.4",
		"gpt-5.3-codex":             "GPT-5.3 Codex",
		"gpt-4.1":                   "GPT-4.1",
		"gpt-5.2":                   "GPT-5.2",
		"anthropic/claude-opus-4-8": "Opus 4.8",
		"claude-4.5-sonnet":         "Sonnet 4.5", // Cursor-style reversed id
		// Legacy Anthropic ids put the version first too; nx renders them as
		// "Sonnet 3.5"-style names (an intentional improvement over tmax).
		"claude-3-5-sonnet-20241022": "Sonnet 3.5",
		"claude-3-opus-20240229":     "Opus 3",
		"claude-3-7-sonnet-20250219": "Sonnet 3.7",
		"auto":                       "Auto",
		"default":                    "Auto",
		"":                           "—",
	}
	for in, want := range cases {
		if got := FriendlyModel(in); got != want {
			t.Errorf("FriendlyModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int64]string{
		0:                 "0",
		999:               "999",
		1000:              "1K",
		35_100_000:        "35.1M",
		2_643_296_342:     "2.6B",
		123_528:           "123.5K",
		999_999:           "1M", // promote, not "1000K"
		999_950_000:       "1B", // promote, not "1000M"
		2_600_000_000_000: "2.6T",
	}
	for in, want := range cases {
		if got := FormatTokens(in); got != want {
			t.Errorf("FormatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		587:     "587",
		130502:  "130,502",
		1000000: "1,000,000",
		-1234:   "-1,234",
	}
	for in, want := range cases {
		if got := FormatInt(in); got != want {
			t.Errorf("FormatInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatHour(t *testing.T) {
	cases := map[int]string{
		0:  "12 AM",
		1:  "1 AM",
		11: "11 AM",
		12: "12 PM",
		13: "1 PM",
		23: "11 PM",
		-1: "—",
	}
	for in, want := range cases {
		if got := FormatHour(in); got != want {
			t.Errorf("FormatHour(%d) = %q, want %q", in, got, want)
		}
	}
}

// agg builds an aggregate whose active days are the given local dates.
func aggWithDays(dates ...string) *Aggregate {
	a := newAggregate(Combined)
	for _, d := range dates {
		a.ByDayMsgs[d] = 1
		a.ByDayTokens[d] = 1000
	}
	return a
}

func TestStreaks(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.Local)

	// today + 6 prior consecutive days => current 7, longest 7
	a := aggWithDays("2026-06-29", "2026-06-28", "2026-06-27", "2026-06-26", "2026-06-25", "2026-06-24", "2026-06-23")
	if cur, long := a.Streaks(now); cur != 7 || long != 7 {
		t.Errorf("consecutive: cur=%d long=%d, want 7/7", cur, long)
	}

	// gap: today active, then a 2-day hole, then a longer earlier run
	b := aggWithDays("2026-06-29", "2026-06-20", "2026-06-19", "2026-06-18", "2026-06-17")
	if cur, long := b.Streaks(now); cur != 1 || long != 4 {
		t.Errorf("gapped: cur=%d long=%d, want 1/4", cur, long)
	}

	// today missing but yesterday active => current streak counts from yesterday
	c := aggWithDays("2026-06-28", "2026-06-27")
	if cur, long := c.Streaks(now); cur != 2 || long != 2 {
		t.Errorf("yesterday: cur=%d long=%d, want 2/2", cur, long)
	}

	// stale: last active >1 day ago => current 0
	d := aggWithDays("2026-06-20", "2026-06-19")
	if cur, long := d.Streaks(now); cur != 0 || long != 2 {
		t.Errorf("stale: cur=%d long=%d, want 0/2", cur, long)
	}

	// empty
	e := newAggregate(Combined)
	if cur, long := e.Streaks(now); cur != 0 || long != 0 {
		t.Errorf("empty: cur=%d long=%d, want 0/0", cur, long)
	}
}

func TestSummarizeWindow(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.Local)
	a := newAggregate(Combined)
	a.Sessions = 3
	a.Messages = 100
	a.InputTokens = 1000
	a.OutputTokens = 500
	// day inside 7d window and day outside it
	a.ByDayMsgs["2026-06-28"] = 40
	a.ByDayTokens["2026-06-28"] = 700
	a.ByDayMsgs["2026-06-01"] = 60
	a.ByDayTokens["2026-06-01"] = 800

	all := Summarize(a, RangeAll, now)
	if all.TotalTokens != a.TotalTokens() {
		t.Errorf("all-range total = %d, want %d", all.TotalTokens, a.TotalTokens())
	}
	if all.ActiveDays != 2 {
		t.Errorf("all-range active days = %d, want 2", all.ActiveDays)
	}

	week := Summarize(a, Range7d, now)
	if week.ActiveDays != 1 {
		t.Errorf("7d active days = %d, want 1 (only 06-28)", week.ActiveDays)
	}
	if week.TotalTokens != 700 {
		t.Errorf("7d tokens = %d, want 700", week.TotalTokens)
	}
	if week.Messages != 40 {
		t.Errorf("7d messages = %d, want 40", week.Messages)
	}
}

func TestHeatmapShape(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.Local)
	a := aggWithDays("2026-06-29")
	h := buildHeatmap(a, RangeAll, now)
	if h.Weeks != 22 {
		t.Errorf("weeks = %d, want 22", h.Weeks)
	}
	// FirstDay is the Sunday of the leftmost column and drives month labels.
	if h.FirstDay.Weekday() != time.Sunday {
		t.Errorf("FirstDay = %v, want a Sunday", h.FirstDay)
	}
	for r := 0; r < 7; r++ {
		if len(h.Cells[r]) != h.Weeks {
			t.Fatalf("row %d has %d cols, want %d", r, len(h.Cells[r]), h.Weeks)
		}
	}
	// today's cell (last column, row = weekday) should be lit
	today := now.Local()
	row := int(today.Weekday())
	if h.Cells[row][h.Weeks-1] <= 0 {
		t.Errorf("today's cell not lit: %d", h.Cells[row][h.Weeks-1])
	}
	// a future cell (row after today in last column) must be out of range
	if today.Weekday() < 6 {
		if h.Cells[row+1][h.Weeks-1] != -1 {
			t.Errorf("future cell should be -1, got %d", h.Cells[row+1][h.Weeks-1])
		}
	}
}

func TestHobbitLine(t *testing.T) {
	if got := HobbitLine(0); got == "" {
		t.Error("zero factor produced empty line")
	}
	if got := HobbitLine(285); got != "You've used ~285× more tokens than The Hobbit." {
		t.Errorf("285 => %q", got)
	}
	if got := HobbitLine(0.5); got != "You've used ~50% of the tokens in The Hobbit." {
		t.Errorf("0.5 => %q", got)
	}
}

func TestMergeTotals(t *testing.T) {
	a := newAggregate(Claude)
	a.Sessions, a.Messages, a.InputTokens = 2, 10, 100
	a.ByDayMsgs["2026-06-29"] = 5
	b := newAggregate(Codex)
	b.Sessions, b.Messages, b.OutputTokens = 3, 20, 200
	b.ByDayMsgs["2026-06-29"] = 7
	a.Merge(b)
	if a.Sessions != 5 || a.Messages != 30 {
		t.Errorf("merged sessions/messages = %d/%d, want 5/30", a.Sessions, a.Messages)
	}
	if a.TotalTokens() != 300 {
		t.Errorf("merged tokens = %d, want 300", a.TotalTokens())
	}
	if a.ByDayMsgs["2026-06-29"] != 12 {
		t.Errorf("merged day msgs = %d, want 12", a.ByDayMsgs["2026-06-29"])
	}
}
