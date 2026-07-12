package ui

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/o1x3/nx/internal/token/core"

	"charm.land/lipgloss/v2"
)

// Tabs — body views selectable by flag and cycled in the interactive TUI.
const (
	TabOverview  = "overview"
	TabModels    = "models"
	TabHours     = "hours"
	TabPunchcard = "punchcard"
	TabTrend     = "trend"
	TabTopDays   = "topdays"
	TabWeekday   = "weekday"
	TabCost      = "cost"
	TabMix       = "mix"
)

// TabOrder is the canonical cycle order, also used for the header view strip.
var TabOrder = []string{
	TabOverview, TabModels, TabHours, TabPunchcard, TabTrend,
	TabTopDays, TabWeekday, TabCost, TabMix,
}

var tabTitles = map[string]string{
	TabOverview:  "Overview",
	TabModels:    "Models",
	TabHours:     "Hours",
	TabPunchcard: "Punchcard",
	TabTrend:     "Trend",
	TabTopDays:   "Busiest Days",
	TabWeekday:   "Weekday",
	TabCost:      "Cost",
	TabMix:       "Mix",
}

// tabShort are the compact labels for the dim cycle strip under the header.
// Kept terse so the whole strip stays within the width budget (≤72 cols).
var tabShort = map[string]string{
	TabOverview:  "overview",
	TabModels:    "models",
	TabHours:     "hours",
	TabPunchcard: "punch",
	TabTrend:     "trend",
	TabTopDays:   "busy",
	TabWeekday:   "weekday",
	TabCost:      "cost",
	TabMix:       "mix",
}

// TabTitle is the display name for a tab const, defaulting to Overview.
func TabTitle(tab string) string {
	if t, ok := tabTitles[tab]; ok {
		return t
	}
	return tabTitles[TabOverview]
}

const (
	contentW  = 72 // nominal width used only to right-align the range + footer
	logoW     = 18 // neofetch logo column (ANSI-Shadow wordmark is exactly 18)
	bannerGap = 3  // spaces between logo and info columns
	infoW     = 33 // the key·value info column (values right-align here)
	gutterW   = 4  // weekday gutter ("Mon ") on the contribution graph
)

// logoArt is the "nx" wordmark (pyfiglet "ANSI Shadow"). Every row is exactly
// logoW cells wide; recoloured per harness via the accent, never per-harness.
var logoArt = [6]string{
	"███╗   ██╗██╗  ██╗",
	"████╗  ██║╚██╗██╔╝",
	"██╔██╗ ██║ ╚███╔╝ ",
	"██║╚██╗██║ ██╔██╗ ",
	"██║ ╚████║██╔╝ ██╗",
	"╚═╝  ╚═══╝╚═╝  ╚═╝",
}

func styled(fg color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(fg) }

// ascii reports whether block cells should fall back to printable shade
// glyphs (piped / no-colour output). Set via Configure.
func ascii() bool { return cfgPlain }

// RenderCard renders the full dashboard for a summary. No background, no border:
// every line is foreground colour on the terminal's own background, with a
// ragged right edge — it sits in your terminal the way fastfetch does.
func RenderCard(s core.Summary, tab string) string {
	th := ThemeFor(s.Harness)

	blocks := []string{
		renderHeader(th, tab, s.Range),
		renderTabStrip(th, tab),
		"",
		renderBanner(th, s),
		"",
	}
	blocks = append(blocks, renderBody(th, s, tab))
	blocks = append(blocks, "", renderFooter(th, s), "", renderSwatches())

	return strings.Join(blocks, "\n")
}

// renderBody dispatches to the active tab's renderer. The default (overview)
// shows the contribution heatmap; every other tab is a self-contained view of
// the same windowed data.
func renderBody(th Theme, s core.Summary, tab string) string {
	switch tab {
	case TabModels:
		return renderModels(th, s)
	case TabHours:
		return renderHours(th, s)
	case TabPunchcard:
		return renderPunchcard(th, s)
	case TabTrend:
		return renderTrend(th, s)
	case TabTopDays:
		return renderTopDays(th, s)
	case TabWeekday:
		return renderWeekday(th, s)
	case TabCost:
		return renderCost(th, s)
	case TabMix:
		return renderMix(th, s)
	default:
		return sectionTitle(th, s.Heatmap.Weeks) + "\n" + renderHeatmap(th, s.Heatmap)
	}
}

// rightAlign places right at the far edge of a w-wide line, left at the start,
// padded with plain spaces between (invisible without a background).
func rightAlign(left, right string, w int) string {
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// ---- header: tabs (left) + range (right) ----

func renderHeader(th Theme, tab, rng string) string {
	// A single active-view breadcrumb scales to nine tabs where a chip-per-tab
	// bar would not; the full list lives in the dim strip below (renderTabStrip).
	chip := styled(th.Accent).Bold(true).Render("[ " + TabTitle(tab) + " ]")
	return rightAlign(chip, renderRange(th, rng), contentW)
}

// renderTabStrip is the dim cycle list under the header. The active view is
// bolded in the accent; it restores the at-a-glance list of views the old
// two-chip header gave, now that there are nine of them.
func renderTabStrip(th Theme, tab string) string {
	parts := make([]string, 0, len(TabOrder))
	for _, t := range TabOrder {
		lbl := tabShort[t]
		if t == tab {
			parts = append(parts, styled(th.Accent).Bold(true).Render(lbl))
		} else {
			parts = append(parts, styled(muted()).Render(lbl))
		}
	}
	return strings.Join(parts, styled(muted()).Render(" · "))
}

func renderRange(th Theme, rng string) string {
	plain := ascii()
	seg := func(key, lbl string) string {
		if rng == key {
			st := styled(th.Accent).Bold(true)
			if plain {
				st = st.Underline(true) // distinguish the active range when piped
			}
			return st.Render(lbl)
		}
		return styled(muted()).Render(lbl)
	}
	return seg(core.RangeAll, "All") + "  " + seg(core.Range30d, "30d") + "  " + seg(core.Range7d, "7d")
}

// ---- neofetch banner: logo column | key·value info column ----

func renderBanner(th Theme, s core.Summary) string {
	host := hostUser()
	title := styled(th.Accent).Bold(true).Render(host) +
		styled(muted()).Render("@") +
		styled(th.Accent).Bold(true).Render(th.Name)
	underline := styled(th.Accent).Render(strings.Repeat("─", lipgloss.Width(host)+1+lipgloss.Width(th.Name)))

	tokens := core.FormatTokens(s.TotalTokens)
	if s.TokensEstimated {
		tokens = "≈" + tokens // cursor stores no token counts; figures are bytes/4
	}
	info := []string{
		title,
		underline,
		leaderRow("sessions", core.FormatInt(s.Sessions), infoW),
		leaderRow("messages", core.FormatInt(s.Messages), infoW),
		leaderRow("tokens", tokens, infoW),
		leaderRow("active days", core.FormatInt(s.ActiveDays), infoW),
		leaderRow("streak", fmt.Sprintf("%dd / %dd", s.CurrentStreak, s.LongestStreak), infoW),
		leaderRow("peak hour", core.FormatHour(s.PeakHour), infoW),
		leaderRow("fav model", s.FavModel, infoW),
	}

	gap := strings.Repeat(" ", bannerGap)
	rows := make([]string, len(info))
	for i := range info {
		logo := strings.Repeat(" ", logoW)
		if i < len(logoArt) {
			logo = styled(th.Accent).Render(logoArt[i])
		}
		rows[i] = logo + gap + info[i]
	}
	return strings.Join(rows, "\n")
}

// leaderRow renders a neofetch-style "key ···· value" line exactly colW wide, so
// the value right-aligns to a shared column. The value is truncated to fit.
func leaderRow(key, val string, colW int) string {
	kW := lipgloss.Width(key)
	maxVal := max(colW-kW-3, 1) // space + at least one dot + space
	val = truncate(val, maxVal)
	n := max(colW-kW-lipgloss.Width(val)-2, 1)
	return styled(label()).Render(key) +
		styled(muted()).Render(" "+strings.Repeat("·", n)+" ") +
		styled(value()).Bold(true).Render(val)
}

// ---- contribution graph (GitHub-style hero) ----

func sectionTitle(th Theme, weeks int) string {
	return styled(th.Accent).Bold(true).Render("Contributions") +
		styled(muted()).Render(fmt.Sprintf(" · last %d weeks", weeks))
}

func renderHeatmap(th Theme, h core.Heatmap) string {
	if h.Weeks <= 0 {
		return ""
	}
	// Defensive: a grid row is gutterW + 3*cols - 1 cells wide; production passes 22.
	cols := h.Weeks
	if maxCols := (contentW - gutterW + 1) / 3; cols > maxCols {
		cols = maxCols
	}
	plain := ascii()
	gut := [7]string{"    ", "Mon ", "    ", "Wed ", "    ", "Fri ", "    "}

	rows := make([]string, 0, 10)
	rows = append(rows, renderMonthRow(h, cols))
	for r := range 7 {
		var sb strings.Builder
		sb.WriteString(styled(label()).Render(gut[r]))
		for col := range cols {
			if col > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(heatCell(th, h.Cells[r][col], h.Max, plain))
		}
		rows = append(rows, sb.String())
	}
	rows = append(rows, "", renderLegend(th, plain))
	return strings.Join(rows, "\n")
}

func heatCell(th Theme, v, max int64, plain bool) string {
	if v < 0 { // future → blank
		return "  "
	}
	if plain {
		return shadeGlyphs[th.levelIndex(v, max)]
	}
	return styled(th.level(v, max)).Render("██") // foreground block, no background
}

// renderMonthRow places 3-letter month abbreviations above the column where each
// month begins. If the first column collides with the previous label the month
// is deferred to its next column rather than dropped (placed advances only after
// a label is written).
func renderMonthRow(h core.Heatmap, cols int) string {
	rowW := gutterW + cols*3
	buf := make([]rune, rowW)
	for i := range buf {
		buf[i] = ' '
	}
	lastEnd := -10
	placed := time.Month(0)
	for col := range cols {
		d := h.FirstDay.AddDate(0, 0, col*7)
		if d.Month() == placed {
			continue
		}
		x := gutterW + col*3
		if x < lastEnd+1 || x+3 > rowW {
			continue
		}
		ab := d.Format("Jan")
		for i := range len(ab) {
			buf[x+i] = rune(ab[i])
		}
		placed = d.Month()
		lastEnd = x + 3
	}
	return styled(label()).Render(strings.TrimRight(string(buf), " "))
}

func renderLegend(th Theme, plain bool) string {
	cell := func(i int) string {
		if plain {
			return shadeGlyphs[i]
		}
		return styled(th.Ramp[i]).Render("██")
	}
	parts := []string{styled(muted()).Render("Less ")}
	for i := range 5 {
		if i > 0 {
			parts = append(parts, " ")
		}
		parts = append(parts, cell(i))
	}
	parts = append(parts, styled(muted()).Render(" More"))
	legend := strings.Join(parts, "")

	pad := max(contentW-lipgloss.Width(legend), 0)
	return strings.Repeat(" ", pad) + legend
}

// ---- neofetch palette: two rows of the terminal's own ANSI colours ----

func renderSwatches() string {
	row := func(start int) string {
		var sb strings.Builder
		for i := range 8 {
			if i > 0 {
				sb.WriteString(" ")
			}
			// lipgloss.Color("0".."15") yields a basic ANSI colour, which keeps
			// the classic 30–37/90–97 SGR codes (same bytes as the v1 renderer).
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(strconv.Itoa(start + i))).Render("██"))
		}
		return sb.String()
	}
	return row(0) + "\n" + row(8)
}

// ---- models tab ----

func renderModels(th Theme, s core.Summary) string {
	if len(s.Models) == 0 {
		return styled(muted()).Render("No model usage recorded.")
	}
	models := s.Models
	if len(models) > 8 {
		models = models[:8]
	}
	var max int64 = 1
	for _, m := range models {
		if m.Tokens > max {
			max = m.Tokens
		}
	}
	var total int64
	for _, m := range s.Models {
		total += m.Tokens
	}

	const nameW = 16
	const metaW = 16 // "  " + 7 tokens + "  " + 5 pct
	barW := contentW - nameW - metaW
	rows := []string{styled(th.Accent).Bold(true).Render("Token share by model"), ""}
	for _, m := range models {
		name := styled(value()).Bold(true).Render(padRight(truncate(m.Name, nameW), nameW))

		filled := int(float64(m.Tokens) / float64(max) * float64(barW))
		if filled < 1 && m.Tokens > 0 {
			filled = 1
		}
		filled = min(filled, barW)
		// filled run in the ramp colour; the rest is plain spaces (no heavy track)
		bar := styled(th.level(m.Tokens, max)).Render(strings.Repeat("█", filled)) +
			strings.Repeat(" ", barW-filled)

		pct := "—"
		if total > 0 {
			p := float64(m.Tokens) / float64(total) * 100
			if p >= 100 {
				pct = "100%"
			} else {
				pct = fmt.Sprintf("%.1f%%", p)
			}
		}
		meta := styled(label()).Render("  "+padLeft(truncate(core.FormatTokens(m.Tokens), 7), 7)+"  ") +
			styled(muted()).Render(padLeft(pct, 5))

		rows = append(rows, name+bar+meta)
	}
	return strings.Join(rows, "\n")
}

// ---- footer ----

func renderFooter(th Theme, s core.Summary) string {
	marker := styled(th.Accent).Render("› ")

	tag := strings.ToLower(s.Harness)
	if tag == core.Combined {
		tag = "all"
	}
	right := styled(th.Accent).Render(tag + " · " + rangeLabel(s.Range))

	hobMax := contentW - lipgloss.Width(marker) - lipgloss.Width(right) - 2
	hob := truncate(core.HobbitLine(s.HobbitFactor), hobMax)
	left := marker + styled(muted()).Italic(true).Render(hob)

	return rightAlign(left, right, contentW)
}

func rangeLabel(r string) string {
	switch r {
	case core.Range7d:
		return "last 7d"
	case core.Range30d:
		return "last 30d"
	default:
		return "all-time"
	}
}

// Hint renders a dim helper line shown under the card on a TTY.
func Hint(s string) string {
	return lipgloss.NewStyle().Foreground(muted()).Italic(true).Render("  " + s)
}

// ---- small helpers ----

func hostUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "you"
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:1]
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func padRight(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

func padLeft(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return strings.Repeat(" ", d) + s
}
