package ui

import (
	"image/color"
	"math"
	"strings"
)

// sparkBlocks are the eight one-cell heights used for unicode sparklines, low
// to high. They are printable in every terminal, so (like the heatmap shade
// glyphs) they survive colour stripping when output is piped.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// shadeGlyphs1 is the single-cell counterpart of the heatmap's shadeGlyphs,
// indexed by a 0..4 ramp level. Used where one column per cell is the budget
// (the punch card) rather than the heatmap's two.
var shadeGlyphs1 = [5]string{" ", "░", "▒", "▓", "█"}

// sparkline renders vals as a one-cell-tall unicode sparkline exactly len(vals)
// cells wide. Height encodes magnitude relative to the series max; each cell is
// also tinted by the theme ramp so colour reinforces height. A zero cell shows
// the lowest block in the muted colour so gaps read as a flat baseline rather
// than vanishing.
//
// Heights bucket on a LOG scale, not linearly: token counts routinely span an
// order of magnitude day to day, and linear bucketing would flatten every
// ordinary day to one pixel under a single spike. log1p compresses that range
// so the shape of typical days stays legible.
func sparkline(th Theme, vals []int64) string {
	if len(vals) == 0 {
		return ""
	}
	var max int64
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	plain := ascii()
	denom := math.Log1p(float64(max))
	var sb strings.Builder
	for _, v := range vals {
		if v <= 0 {
			if plain {
				sb.WriteRune(sparkBlocks[0])
			} else {
				sb.WriteString(styled(muted()).Render(string(sparkBlocks[0])))
			}
			continue
		}
		// 1..7 so any non-zero day sits visibly above the baseline; the top
		// block (7) is reserved for the window max.
		idx := 1
		if denom > 0 {
			idx = 1 + int(math.Round(math.Log1p(float64(v))/denom*float64(len(sparkBlocks)-2)))
		}
		idx = min(idx, len(sparkBlocks)-1)
		g := string(sparkBlocks[idx])
		if plain {
			sb.WriteString(g)
		} else {
			// reuse the heatmap ramp so a sparkline matches the card's palette
			sb.WriteString(styled(th.level(v, max)).Render(g))
		}
	}
	return sb.String()
}

// hbar renders a horizontal bar: `filled` ramp-coloured blocks padded with
// spaces to `width` (no heavy track, matching the models tab). filled is
// clamped to [0,width]; a positive value always shows at least one block.
func hbar(c color.Color, filled, width int) string {
	if width <= 0 {
		return ""
	}
	filled = max(min(filled, width), 0)
	return styled(c).Render(strings.Repeat("█", filled)) + strings.Repeat(" ", width-filled)
}

// barCells maps a value to a filled-cell count over [0,max] across width cells,
// guaranteeing at least one cell for any positive value.
func barCells(v, max int64, width int) int {
	if max <= 0 || v <= 0 {
		return 0
	}
	n := int(float64(v) / float64(max) * float64(width))
	if n < 1 {
		n = 1
	}
	if n > width {
		n = width
	}
	return n
}
