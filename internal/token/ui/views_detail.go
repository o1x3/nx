package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/o1x3/nx/internal/token/core"
)

var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// densityBar renders a horizontal bar `width` cells wide with v/max of it
// filled. In colour the fill is solid blocks in the ramp colour; when colour is
// stripped the fill glyph reflects the bar's ramp LEVEL (░▒▓█) so intensity —
// not just length — still reads. Full-cell glyphs only, so it pipes identically.
func densityBar(th Theme, v, max int64, width int) string {
	filled := barCells(v, max, width)
	if ascii() {
		g := shadeGlyphs1[th.levelIndex(v, max)]
		if filled > 0 && g == " " {
			g = "░"
		}
		return strings.Repeat(g, filled) + strings.Repeat(" ", width-filled)
	}
	return hbar(th.level(v, max), filled, width)
}

// densityBarF is densityBar for already-floating values (e.g. dollars): it
// preserves the v/max ratio by scaling into the integer ramp.
func densityBarF(th Theme, v, max float64, width int) string {
	if max <= 0 || v <= 0 {
		return strings.Repeat(" ", width)
	}
	return densityBar(th, int64(v/max*1e6), 1e6, width)
}

// ---- hours: 24-row local-hour histogram ----

func renderHours(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("Activity by local hour")
	var max, total int64
	for _, n := range s.Hours {
		total += int64(n)
		if int64(n) > max {
			max = int64(n)
		}
	}
	if total == 0 {
		return title + "\n" + styled(muted()).Render("No activity in this window.")
	}
	// The count column auto-sizes to the widest formatted count: an all-time
	// hour bucket can exceed 99,999 → "100,000" and overflow a fixed-5 field.
	cw := 1
	for _, n := range s.Hours {
		if w := len(core.FormatInt(n)); w > cw {
			cw = w
		}
	}
	const barW = 46
	plain := ascii()
	rows := []string{title, ""}
	for h := range 24 {
		n := int64(s.Hours[h])
		bar := densityBar(th, n, max, barW)
		lbl := padRight(core.FormatHour(h), 5)
		count := "  " + padLeft(core.FormatInt(s.Hours[h]), cw)
		if h == s.PeakHour {
			line := styled(th.Accent).Bold(true).Render(lbl) + " " + bar + styled(th.Accent).Bold(true).Render(count)
			if plain {
				line += styled(th.Accent).Render(" ‹peak")
			}
			rows = append(rows, line)
			continue
		}
		rows = append(rows, styled(label()).Render(lbl)+" "+bar+styled(label()).Render(count))
	}
	var evening int64
	for h := 18; h < 24; h++ {
		evening += int64(s.Hours[h])
	}
	pct := int(math.Round(float64(evening) / float64(total) * 100))
	rows = append(rows, "", styled(muted()).Render(fmt.Sprintf("peak %s · %d%% after 6 PM", core.FormatHour(s.PeakHour), pct)))
	return strings.Join(rows, "\n")
}

// ---- punchcard: weekday × hour density grid ----

func punchLevel(v, max int64) int {
	if v <= 0 {
		return 0
	}
	if max <= 1 {
		return 4
	}
	// log so one busy hour doesn't wash the rest of the grid out
	lvl := 1 + int(math.Log1p(float64(v))/math.Log1p(float64(max))*3)
	return min(lvl, 4)
}

func punchCell(th Theme, v, max int64, peak, plain bool) string {
	if v <= 0 {
		if plain {
			return "·" // distinct zero glyph so the grid still reads when piped
		}
		return styled(muted()).Render("·")
	}
	if peak {
		if plain {
			return "█"
		}
		return styled(th.Accent).Bold(true).Render("█")
	}
	lvl := punchLevel(v, max)
	if plain {
		return shadeGlyphs1[lvl]
	}
	return styled(th.Ramp[lvl]).Render("█")
}

func renderPunchcard(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("When you work") + styled(muted()).Render(" · weekday × hour")
	var gridMax, peakWd, peakHr int
	for wd := range 7 {
		for h := range 24 {
			if v := s.Punch[wd][h]; v > gridMax {
				gridMax, peakWd, peakHr = v, wd, h
			}
		}
	}
	if gridMax == 0 {
		return title + "\n" + styled(muted()).Render("No activity in this window.")
	}
	plain := ascii()

	// hour ruler aligned under the 4-space weekday gutter
	ruler := []rune(strings.Repeat(" ", 24))
	put := func(pos int, s string) {
		for i := 0; i < len(s) && pos+i < 24; i++ {
			ruler[pos+i] = rune(s[i])
		}
	}
	put(0, "0")
	put(6, "6")
	put(12, "12")
	put(18, "18")
	put(22, "23")

	var maxRow int64
	rowSums := [7]int64{}
	for wd := range 7 {
		for h := range 24 {
			rowSums[wd] += int64(s.Punch[wd][h])
		}
		if rowSums[wd] > maxRow {
			maxRow = rowSums[wd]
		}
	}

	gut := [7]string{"    ", "Mon ", "    ", "Wed ", "    ", "Fri ", "    "}
	rows := []string{title, "", styled(label()).Render("    " + string(ruler))}
	for wd := range 7 {
		var sb strings.Builder
		sb.WriteString(styled(label()).Render(gut[wd]))
		for h := range 24 {
			sb.WriteString(punchCell(th, int64(s.Punch[wd][h]), int64(gridMax), wd == peakWd && h == peakHr, plain))
		}
		sb.WriteString("  ") // right-margin weekday marginal
		sb.WriteString(punchCell(th, rowSums[wd], maxRow, false, plain))
		rows = append(rows, sb.String())
	}
	rows = append(rows, "", styled(muted()).Render(fmt.Sprintf("busiest: %s %s · activity, not tokens", weekdayNames[peakWd], core.FormatHour(peakHr))))
	return strings.Join(rows, "\n")
}

// ---- trend: daily sparkline + averages + momentum ----

func renderTrend(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("Daily tokens") + styled(muted()).Render(fmt.Sprintf(" · last %dd", s.DailyDays))
	var total, max int64
	for _, v := range s.Daily {
		total += v
		if v > max {
			max = v
		}
	}
	if total == 0 {
		return title + "\n" + styled(muted()).Render("No tokens in this window.")
	}
	rows := []string{title, "", sparkline(th, s.Daily), ""}
	if s.ActiveDays > 0 {
		rows = append(rows, leaderRow("avg / active day", core.FormatTokens(s.TotalTokens/int64(s.ActiveDays)), 40))
	}
	if s.Range != core.RangeAll && s.DailyDays > 0 {
		rows = append(rows, leaderRow("avg / day", core.FormatTokens(s.TotalTokens/int64(s.DailyDays)), 40))
	}
	if len(s.TopDays) > 0 {
		d := s.TopDays[0]
		rows = append(rows, leaderRow("busiest day", d.Date.Format("Mon Jan 2")+" · "+core.FormatTokens(d.Tokens), 40))
	}
	// week-over-week momentum needs ≥14 days of contiguous series
	if len(s.Daily) >= 14 {
		n := len(s.Daily)
		var last7, prev7 int64
		for _, v := range s.Daily[n-7:] {
			last7 += v
		}
		for _, v := range s.Daily[n-14 : n-7] {
			prev7 += v
		}
		rows = append(rows, leaderRow("last 7d vs prev 7d", wowDelta(last7, prev7), 40))
	}
	return strings.Join(rows, "\n")
}

// wowDelta formats a week-over-week change, guarding the prior==0 +Inf% case.
func wowDelta(last, prev int64) string {
	if prev == 0 {
		if last == 0 {
			return "—"
		}
		return "new"
	}
	pct := float64(last-prev) / float64(prev) * 100
	if pct >= 0 {
		return fmt.Sprintf("+%.0f%%", pct)
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// ---- busiest days: ranked table with inline bars ----

func renderTopDays(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("Busiest days")
	if len(s.TopDays) == 0 {
		return title + "\n" + styled(muted()).Render("No activity in this window.")
	}
	var max int64 = 1 // guard: TopDaysIn can include zero-token, message-only days
	for _, d := range s.TopDays {
		if d.Tokens > max {
			max = d.Tokens
		}
	}
	const barW = 24
	rows := []string{title, ""}
	for _, d := range s.TopDays {
		date := styled(value()).Bold(true).Render(padRight(d.Date.Format("Mon Jan 2"), 11))
		var bar, toks string
		if d.Tokens > 0 {
			bar = densityBar(th, d.Tokens, max, barW)
			toks = padLeft(core.FormatTokens(d.Tokens), 7)
		} else {
			bar = strings.Repeat(" ", barW) // message-only day (e.g. Codex)
			toks = padLeft("—", 7)
		}
		msgs := padLeft(core.FormatInt(d.Messages)+" msg", 10)
		rows = append(rows, date+"  "+bar+styled(label()).Render("  "+toks)+styled(muted()).Render("  "+msgs))
	}
	return strings.Join(rows, "\n")
}

// ---- weekday: Mon-first token bars + skew verdict ----

func renderWeekday(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("Tokens by weekday")
	order := []int{1, 2, 3, 4, 5, 6, 0} // Mon..Sun, indexing the Sun-first arrays
	labels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	var max, total int64
	for _, v := range s.WeekdayTok {
		total += v
		if v > max {
			max = v
		}
	}
	if total == 0 {
		return title + "\n" + styled(muted()).Render("No activity in this window.")
	}
	const barW = 46
	rows := []string{title, ""}
	busiest, bestTok := labels[0], int64(-1)
	for i, idx := range order {
		tok, msg := s.WeekdayTok[idx], s.WeekdayMsg[idx]
		if tok > bestTok {
			bestTok, busiest = tok, labels[i]
		}
		bar := densityBar(th, tok, max, barW)
		meta := styled(label()).Render("  "+padLeft(core.FormatTokens(tok), 7)) +
			styled(muted()).Render("  "+padLeft(core.FormatInt(msg), 6))
		rows = append(rows, styled(value()).Render(padRight(labels[i], 4))+bar+meta)
	}
	weekend := s.WeekdayTok[0] + s.WeekdayTok[6] // Sun + Sat
	wpct := int(math.Round(float64(weekend) / float64(total) * 100))
	rows = append(rows, "", styled(muted()).Render(fmt.Sprintf("weekend %d%% · busiest %s", wpct, busiest)))
	return strings.Join(rows, "\n")
}

// ---- cost: estimated spend ----

func renderCost(th Theme, s core.Summary) string {
	cb := s.Cost
	title := styled(th.Accent).Bold(true).Render("Estimated spend")
	if cb.Total <= 0 {
		return title + "\n" + styled(muted()).Render("No priced usage in this window.")
	}
	head := rightAlign(title, styled(th.Accent).Bold(true).Render("≈ "+core.FormatUSD(cb.Total)), contentW)
	rows := []string{head, ""}

	var max float64
	for _, m := range cb.Models {
		if m.USD > max {
			max = m.USD
		}
	}
	const usdW, gapW, minBarW = 9, 2, 12 // amount + "  " + bar floor
	shown := cb.Models
	if len(shown) > 8 {
		shown = shown[:8]
	}
	names := make([]string, 0, len(shown))
	for _, m := range shown {
		if m.USD > 0 {
			names = append(names, m.Name)
		}
	}
	nameW := fitNameWidth(names, 8, contentW-gapW-usdW-minBarW)
	barW := contentW - nameW - gapW - usdW
	for _, m := range shown {
		if m.USD <= 0 {
			continue
		}
		name := styled(value()).Bold(true).Render(padRight(truncate(m.Name, nameW), nameW))
		bar := densityBarF(th, m.USD, max, barW)
		rows = append(rows, name+bar+styled(label()).Render("  "+padLeft(core.FormatUSD(m.USD), usdW)))
	}
	rows = append(rows, "")
	if cb.CacheSavingUSD > 0 {
		rows = append(rows, styled(th.Accent).Render("⌁ ")+
			styled(value()).Bold(true).Render(core.FormatUSD(cb.CacheSavingUSD))+
			styled(muted()).Render(" saved by prompt caching"))
	}
	rows = append(rows, styled(muted()).Render(fmt.Sprintf("tokens: %s in · %s out · %s cache",
		core.FormatTokens(s.InputTokens), core.FormatTokens(s.OutputTokens),
		core.FormatTokens(s.CacheReadTokens+s.CacheWriteTokens))))

	disc := "list prices · per-model split estimated"
	if s.Range != core.RangeAll {
		disc += " · window estimated"
	}
	if s.Harness == core.Combined {
		disc += " · mixed providers"
	}
	if s.TokensEstimated {
		disc += " · cursor tokens estimated"
	}
	rows = append(rows, styled(muted()).Faint(true).Render(disc))
	return strings.Join(rows, "\n")
}

// ---- mix: proportional token composition ----

// largestRemainder apportions `width` cells across vals so the per-segment cell
// counts sum to EXACTLY width (naive rounding drifts to width±1, over/underfill).
func largestRemainder(vals []int64, width int) []int {
	out := make([]int, len(vals))
	var total int64
	for _, v := range vals {
		total += v
	}
	if total <= 0 || width <= 0 {
		return out
	}
	type frac struct {
		i   int
		rem float64
	}
	fracs := make([]frac, len(vals))
	sum := 0
	for i, v := range vals {
		exact := float64(v) / float64(total) * float64(width)
		out[i] = int(exact)
		sum += out[i]
		fracs[i] = frac{i, exact - float64(out[i])}
	}
	sort.Slice(fracs, func(a, b int) bool { return fracs[a].rem > fracs[b].rem })
	for k := 0; sum < width && k < len(fracs); k++ {
		out[fracs[k].i]++
		sum++
	}
	return out
}

func renderMix(th Theme, s core.Summary) string {
	title := styled(th.Accent).Bold(true).Render("Token composition")
	type seg struct {
		name string
		val  int64
		lvl  int // ramp index / shade glyph
	}
	all := []seg{
		{"fresh input", s.InputTokens, 4},
		{"output", s.OutputTokens, 3},
		{"cache read", s.CacheReadTokens, 2},
		{"cache write", s.CacheWriteTokens, 1},
	}
	var total int64
	segs := make([]seg, 0, 4)
	for _, sg := range all {
		total += sg.val
		// Codex never records cache-write; drop a 0% sliver rather than lie.
		if sg.name == "cache write" && sg.val == 0 {
			continue
		}
		segs = append(segs, sg)
	}
	if total == 0 {
		return title + "\n" + styled(muted()).Render("No tokens in this window.")
	}
	plain := ascii()
	const barW = 56
	vals := make([]int64, len(segs))
	for i, sg := range segs {
		vals[i] = sg.val
	}
	widths := largestRemainder(vals, barW)

	var bar strings.Builder
	for i, sg := range segs {
		if widths[i] <= 0 {
			continue
		}
		if plain {
			bar.WriteString(strings.Repeat(shadeGlyphs1[sg.lvl], widths[i]))
		} else {
			bar.WriteString(styled(th.Ramp[sg.lvl]).Render(strings.Repeat("█", widths[i])))
		}
	}
	rows := []string{title, "", bar.String(), ""}
	for _, sg := range segs {
		pct := float64(sg.val) / float64(total) * 100
		var glyph string
		if plain {
			glyph = shadeGlyphs1[sg.lvl]
		} else {
			glyph = styled(th.Ramp[sg.lvl]).Render("█")
		}
		rows = append(rows, glyph+" "+styled(label()).Render(padRight(sg.name, 12))+
			styled(value()).Render(padLeft(core.FormatTokens(sg.val), 7))+
			styled(muted()).Render("  "+padLeft(fmt.Sprintf("%.0f%%", pct), 4)))
	}
	if s.InputTokens+s.CacheReadTokens > 0 {
		eff := int(math.Round(float64(s.CacheReadTokens) / float64(s.InputTokens+s.CacheReadTokens) * 100))
		rows = append(rows, "", styled(value()).Bold(true).Render(fmt.Sprintf("cache efficiency %d%%", eff))+
			styled(muted()).Render(" of input served from cache"))
	}
	if s.Range != core.RangeAll {
		rows = append(rows, styled(muted()).Faint(true).Render("split is the all-time ratio (windowed split unavailable)"))
	}
	if s.TokensEstimated {
		rows = append(rows, styled(muted()).Faint(true).Render("cursor tokens estimated (bytes/4)"))
	}
	return strings.Join(rows, "\n")
}
