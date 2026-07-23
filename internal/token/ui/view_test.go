package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/o1x3/nx/internal/token/core"

	"charm.land/lipgloss/v2"
)

func init() {
	// deterministic colour output for width assertions: dark variants, colour
	// cells (v2 styles always emit truecolor; profile handling happens in the
	// caller's writer, so tests strip SGR before asserting)
	Configure(true, false)
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func dispWidth(s string) int { return lipgloss.Width(ansi.ReplaceAllString(s, "")) }

func sampleSummary(_ string) core.Summary {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.Local)
	a := &core.Aggregate{
		Harness:     core.Combined,
		Sessions:    102,
		Messages:    16400,
		InputTokens: 5_000_000, OutputTokens: 2_000_000,
		CacheReadTokens: 28_000_000,
		ByDayTokens:     map[string]int64{"2026-06-29": 1_000_000, "2026-06-28": 500_000},
		ByDayMsgs:       map[string]int{"2026-06-29": 50, "2026-06-28": 30},
		ByDayHour:       map[string]*[24]int{"2026-06-29": {12: 50}, "2026-06-28": {9: 30}},
		ByDayModelTok: map[string]map[string]int64{
			"2026-06-29": {"claude-opus-4-8": 30_000_000, "gpt-5.4": 5_000_000},
		},
		ByDayModelMsg: map[string]map[string]int{
			"2026-06-29": {"claude-opus-4-8": 100, "gpt-5.4": 20},
		},
	}
	return core.Summarize(a, core.RangeAll, now)
}

func TestRenderCardOverview(t *testing.T) {
	out := RenderCard(sampleSummary(TabOverview), TabOverview)
	// Active-view chip + the dim cycle strip + banner + heatmap + footer.
	for _, want := range []string{"Overview", "models", "hours", "sessions", "tokens", "peak hour", "fav model", "Contributions", "Less", "More", "Hobbit"} {
		if !strings.Contains(out, want) {
			t.Errorf("overview missing %q", want)
		}
	}
}

func TestRenderCardModels(t *testing.T) {
	out := RenderCard(sampleSummary(TabModels), TabModels)
	if !strings.Contains(out, "Token share by model") {
		t.Error("models view missing heading")
	}
	if !strings.Contains(out, "Opus 4.8") {
		t.Error("models view missing top model")
	}
}

// Cost and models bars size the name column to the longest visible label so
// common Cursor ids (GPT-5.6-sol-high, Opus …thinking-high) stay intact.
func TestModelNamesNotTruncated(t *testing.T) {
	s := sampleSummary(TabCost)
	s.Models = []core.ModelStat{
		{ID: "gpt-5.6-sol-high", Name: "GPT-5.6-sol-high", Tokens: 4_000_000},
		{ID: "claude-opus-4-7-thinking-high", Name: "Opus 4.7.thinking-high", Tokens: 2_000_000},
		{ID: "claude-sonnet-4-5-thinking-high", Name: "Thinking 4.5.high", Tokens: 1_000_000},
		{ID: "claude-sonnet-4", Name: "Sonnet 4", Tokens: 500_000},
	}
	s.Cost = core.EstimateCost(s.Models, s.InputTokens, s.OutputTokens, s.CacheReadTokens, s.CacheWriteTokens)

	for _, tab := range []string{TabCost, TabModels} {
		plain := ansi.ReplaceAllString(RenderCard(s, tab), "")
		for _, name := range []string{"GPT-5.6-sol-high", "Opus 4.7.thinking-high", "Thinking 4.5.high", "Sonnet 4"} {
			if !strings.Contains(plain, name) {
				t.Errorf("%s truncated or dropped %q:\n%s", tab, name, plain)
			}
		}
		if strings.Contains(plain, "…") {
			t.Errorf("%s still ellipsizes a model name that fits:\n%s", tab, plain)
		}
		for i, l := range strings.Split(plain, "\n") {
			if w := dispWidth(l); w > 80 {
				t.Errorf("%s line %d width %d exceeds 80: %q", tab, i, w, l)
			}
		}
	}
}

func TestFitNameWidth(t *testing.T) {
	if got := fitNameWidth([]string{"Sonnet 4", "GPT-5.6-sol-high"}, 8, 40); got != len("GPT-5.6-sol-high") {
		t.Errorf("fitNameWidth = %d, want %d", got, len("GPT-5.6-sol-high"))
	}
	if got := fitNameWidth([]string{"tiny"}, 8, 40); got != 8 {
		t.Errorf("fitNameWidth min = %d, want 8", got)
	}
	if got := fitNameWidth([]string{strings.Repeat("x", 100)}, 8, 20); got != 20 {
		t.Errorf("fitNameWidth max = %d, want 20", got)
	}
}

// Every detail tab must render its heading and the active-view chip without
// panicking on the sample summary.
func TestRenderDetailTabs(t *testing.T) {
	heading := map[string]string{
		TabHours:     "Activity by local hour",
		TabPunchcard: "When you work",
		TabTrend:     "Daily tokens",
		TabTopDays:   "Busiest days",
		TabWeekday:   "Tokens by weekday",
		TabCost:      "Estimated spend",
		TabMix:       "Token composition",
	}
	for tab, want := range heading {
		out := RenderCard(sampleSummary(tab), tab)
		if !strings.Contains(out, want) {
			t.Errorf("%s view missing heading %q", tab, want)
		}
		if !strings.Contains(out, TabTitle(tab)) {
			t.Errorf("%s view missing active chip %q", tab, TabTitle(tab))
		}
	}
}

func TestRenderCompare(t *testing.T) {
	s := sampleSummary(TabOverview)
	claude, codex := s, s
	claude.Harness, codex.Harness = core.Claude, core.Codex
	out := RenderCompare([]core.Summary{claude, codex}, core.RangeAll)
	for _, want := range []string{"Harness comparison", "Claude Code", "Codex", "tokens", "share", "est. spend"} {
		if !strings.Contains(out, want) {
			t.Errorf("compare missing %q", want)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := dispWidth(l); w > 80 {
			t.Errorf("compare line %d width %d exceeds 80: %q", i, w, l)
		}
	}
	if hasBackgroundSGR(out) {
		t.Error("compare sets a background colour")
	}
	// empty input shows the friendly empty state, no panic
	if RenderCompare(nil, core.Range7d) == "" {
		t.Error("empty compare returned blank")
	}
}

// Four columns — every concrete harness side by side — must keep each header
// intact and every line inside the 80-col budget (12 + 4×16 = 76).
func TestRenderCompareFourColumns(t *testing.T) {
	s := sampleSummary(TabOverview)
	sums := make([]core.Summary, 0, len(core.Harnesses))
	for _, h := range core.Harnesses {
		c := s
		c.Harness = h
		sums = append(sums, c)
	}
	if len(sums) != 4 {
		t.Fatalf("expected 4 concrete harnesses, got %d", len(sums))
	}
	out := RenderCompare(sums, core.RangeAll)
	for _, want := range []string{"Claude Code", "Codex", "pi.dev", "Cursor"} {
		if !strings.Contains(out, want) {
			t.Errorf("4-col compare missing header %q", want)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := dispWidth(l); w > 80 {
			t.Errorf("4-col compare line %d width %d exceeds 80: %q", i, w, l)
		}
	}
	if hasBackgroundSGR(out) {
		t.Error("4-col compare sets a background colour")
	}
}

// compareColW keeps labelW + n×colW within the 80-col budget for every column
// count the CLI can produce.
func TestCompareColWBudget(t *testing.T) {
	want := map[int]int{1: 40, 2: 30, 3: 20, 4: 16}
	for n, w := range want {
		if got := compareColW(n); got != w {
			t.Errorf("compareColW(%d) = %d, want %d", n, got, w)
		}
		if total := 12 + n*w; total > 80 {
			t.Errorf("%d columns: total width %d exceeds 80", n, total)
		}
	}
}

// When token figures are bytes/4 estimates (cursor), the banner marks the
// total with ≈ and the cost/mix tabs disclose the estimate; exact counts stay
// unmarked.
func TestTokensEstimatedMarkers(t *testing.T) {
	exact := sampleSummary(TabOverview)
	if strings.Contains(RenderCard(exact, TabOverview), "≈") {
		t.Error("overview shows ≈ although tokens are exact")
	}

	est := exact
	est.TokensEstimated = true
	if !strings.Contains(RenderCard(est, TabOverview), "≈") {
		t.Error("overview banner missing ≈ for estimated tokens")
	}
	if !strings.Contains(RenderCard(est, TabMix), "cursor tokens estimated") {
		t.Error("mix tab missing estimated-tokens note")
	}
	if strings.Contains(RenderCard(exact, TabMix), "cursor tokens estimated") {
		t.Error("mix tab notes an estimate although tokens are exact")
	}
	costOut := RenderCard(est, TabCost)
	if strings.Contains(costOut, "Estimated spend") && strings.Contains(costOut, "$") {
		if !strings.Contains(costOut, "cursor tokens estimated") {
			t.Error("cost tab disclaimer missing estimated-tokens note")
		}
	}
	if strings.Contains(RenderCard(exact, TabCost), "cursor tokens estimated") {
		t.Error("cost tab notes an estimate although tokens are exact")
	}
}

// Every icon row must be exactly logoW cells so the banner's info column
// stays aligned beside it.
func TestLogoArtUniformWidth(t *testing.T) {
	for _, h := range append([]string{core.Combined}, core.Harnesses...) {
		art := logoArtFor(h)
		for i, row := range art {
			if w := lipgloss.Width(row); w != logoW {
				t.Errorf("%s logo row %d is %d cells wide, want logoW=%d", h, i, w, logoW)
			}
		}
	}
}

// The card paints no background, so lines are intentionally ragged — but none
// should exceed the terminal width budget (guards against wrapping / overflow).
func TestRenderCardMaxWidth(t *testing.T) {
	for _, tab := range TabOrder {
		out := RenderCard(sampleSummary(tab), tab)
		for i, l := range strings.Split(out, "\n") {
			if w := dispWidth(l); w > 80 {
				t.Errorf("%s line %d width %d exceeds 80 cols:\n%q", tab, i, w, l)
			}
		}
	}
}

// The card must be fully transparent — no SGR sets a background colour, so it
// blends with the terminal (the whole point of the seamless redesign).
func TestRenderCardNoBackground(t *testing.T) {
	for _, tab := range TabOrder {
		if hasBackgroundSGR(RenderCard(sampleSummary(tab), tab)) {
			t.Errorf("%s sets a background colour; output must be transparent", tab)
		}
	}
}

// hasBackgroundSGR parses SGR sequences and reports whether any sets a
// background, correctly skipping 38;2;r;g;b foreground channels (a channel of
// 48 must not be mistaken for the 48 background introducer).
func hasBackgroundSGR(s string) bool {
	for _, m := range ansi.FindAllStringSubmatch(s, -1) {
		ps := strings.Split(strings.Trim(m[0], "\x1b[m"), ";")
		for i := 0; i < len(ps); i++ {
			switch ps[i] {
			case "38", "48": // extended fg/bg: consume the colour spec
				bg := ps[i] == "48"
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else if i+1 < len(ps) && ps[i+1] == "2" {
					i += 4
				}
				if bg {
					return true
				}
			case "40", "41", "42", "43", "44", "45", "46", "47", "49",
				"100", "101", "102", "103", "104", "105", "106", "107":
				return true
			}
		}
	}
	return false
}

// A month that spans several columns must always get a label, even when its
// first column collides with the previous month's label (regression: the label
// used to be dropped entirely ~23% of the time).
func TestMonthRowNoDroppedMonth(t *testing.T) {
	first := time.Date(2024, 9, 29, 0, 0, 0, 0, time.Local) // a Sunday
	h := core.Heatmap{Weeks: 22, FirstDay: first}
	for r := range 7 {
		h.Cells[r] = make([]int64, 22)
	}
	row := ansi.ReplaceAllString(renderMonthRow(h, 22), "")
	for _, m := range []string{"Sep", "Oct", "Nov", "Dec", "Jan", "Feb"} {
		if !strings.Contains(row, m) {
			t.Errorf("month row dropped %q: %q", m, row)
		}
	}
}

// When colour will be stripped (piped output) the heatmap falls back to shade
// glyphs; stripped lines must still stay within budget. Note the v2 semantic:
// plain mode still emits SGR (the caller's writer strips it), so assertions
// run on the SGR-stripped text.
func TestRenderCardAsciiWidth(t *testing.T) {
	Configure(true, true)
	defer Configure(true, false)

	for _, tab := range TabOrder {
		out := RenderCard(sampleSummary(tab), tab)
		for i, l := range strings.Split(out, "\n") {
			if w := dispWidth(l); w > 80 {
				t.Errorf("ascii %s line %d width %d exceeds 80:\n%q", tab, i, w, l)
			}
		}
	}
}

// Plain mode must swap colour-only ██ heatmap cells for printable shade
// glyphs so density still reads once SGR is stripped; colour mode must not
// leak shade cells into the heatmap grid.
func TestPlainModeShadeGlyphs(t *testing.T) {
	Configure(true, true)
	defer Configure(true, false)
	plain := ansi.ReplaceAllString(RenderCard(sampleSummary(TabOverview), TabOverview), "")
	// the legend always shows the full ramp: "Less" ░░ ▒▒ ▓▓ ██ "More"
	for _, g := range []string{"░░", "▒▒", "▓▓", "██"} {
		if !strings.Contains(plain, g) {
			t.Errorf("plain overview missing shade glyph %q", g)
		}
	}

	Configure(true, false)
	colour := ansi.ReplaceAllString(RenderCard(sampleSummary(TabOverview), TabOverview), "")
	for _, g := range []string{"░░", "▒▒", "▓▓"} {
		if strings.Contains(colour, g) {
			t.Errorf("colour overview contains shade glyph %q; cells should be coloured ██", g)
		}
	}
}
