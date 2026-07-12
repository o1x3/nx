package core

import (
	"fmt"
	"sort"
	"strings"
)

// modelPrice is a model's public list price in USD per 1,000,000 tokens, split
// by token class. cacheWrite (prompt-cache creation) is Anthropic-specific; for
// providers that don't bill cache creation separately it equals the input rate.
type modelPrice struct {
	in, out, cacheRead, cacheWrite float64
}

// priceTable is matched by case-insensitive substring against the raw model id,
// MOST SPECIFIC FIRST (so "gpt-5.4-nano" wins over "gpt-5.4" wins over "gpt-5").
// Prices are public list prices; they drift, so cost is always an estimate.
var priceTable = []struct {
	key string
	p   modelPrice
}{
	// Anthropic — Claude (cache read 0.1×, cache write 1.25× of input)
	{"opus", modelPrice{15, 75, 1.5, 18.75}},
	{"sonnet", modelPrice{3, 15, 0.3, 3.75}},
	{"haiku", modelPrice{1, 5, 0.1, 1.25}},
	{"fable", modelPrice{3, 15, 0.3, 3.75}}, // no public price; sonnet-tier estimate

	// OpenAI — GPT-5.x family (no separate cache-creation charge → cacheWrite = in)
	{"gpt-5.4-nano", modelPrice{0.2, 1.25, 0.02, 0.2}},
	{"gpt-5.4-mini", modelPrice{0.75, 4.5, 0.075, 0.75}},
	{"gpt-5.4-pro", modelPrice{30, 180, 30, 30}},
	{"gpt-5.4", modelPrice{2.5, 15, 0.25, 2.5}},
	{"gpt-5.2-pro", modelPrice{21, 168, 21, 21}},
	{"gpt-5.2", modelPrice{1.75, 14, 0.175, 1.75}},
	{"gpt-5.1", modelPrice{1.25, 10, 0.125, 1.25}},
	{"gpt-5-nano", modelPrice{0.05, 0.4, 0.005, 0.05}},
	{"gpt-5-mini", modelPrice{0.25, 2, 0.025, 0.25}},
	{"gpt-5-pro", modelPrice{15, 120, 15, 15}},
	{"gpt-5", modelPrice{1.25, 10, 0.125, 1.25}},

	// OpenAI — GPT-4.x / 4o
	{"gpt-4.1-mini", modelPrice{0.4, 1.6, 0.1, 0.4}},
	{"gpt-4.1-nano", modelPrice{0.1, 0.4, 0.025, 0.1}},
	{"gpt-4.1", modelPrice{2, 8, 0.5, 2}},
	{"gpt-4o-mini", modelPrice{0.15, 0.6, 0.075, 0.15}},
	{"gpt-4o", modelPrice{2.5, 10, 1.25, 2.5}},

	// OpenAI — reasoning o-series (approximate historical list prices)
	{"o4-mini", modelPrice{1.1, 4.4, 0.275, 1.1}},
	{"o3-mini", modelPrice{1.1, 4.4, 0.55, 1.1}},
	{"o3", modelPrice{2, 8, 0.5, 2}},
	{"o1-mini", modelPrice{1.1, 4.4, 0.55, 1.1}},
	{"o1", modelPrice{15, 60, 7.5, 15}},

	// Google — Gemini (approx 2.5 Pro tier)
	{"gemini", modelPrice{1.25, 5, 0.31, 1.25}},
}

// priceFor returns the list price for a raw model id and whether one was found.
// "-codex" suffixes are stripped so Codex variants price as their base model.
func priceFor(id string) (modelPrice, bool) {
	low := strings.ToLower(id)
	low = strings.ReplaceAll(low, "-codex", "")
	for _, e := range priceTable {
		if strings.Contains(low, e.key) {
			return e.p, true
		}
	}
	return modelPrice{}, false
}

// ModelCost is one model's estimated spend within a window.
type ModelCost struct {
	ID     string
	Name   string
	Tokens int64
	USD    float64
	Priced bool
}

// CostBreakdown is the estimated spend for a window. It is an approximation:
// we know the input/output/cache split per harness but only TOTAL tokens per
// model, so each model's tokens are apportioned across the four price classes
// using the harness-wide ratio.
type CostBreakdown struct {
	Total          float64     // summed USD over priced models
	Models         []ModelCost // descending by USD
	PricedToks     int64       // tokens we had a price for
	TotalToks      int64       // all model tokens considered
	AllPriced      bool        // every model had a price
	CacheSavingUSD float64     // saved by serving cache-read tokens vs fresh input
}

// EstimateCost apportions each model's tokens across input/output/cache classes
// using the harness-wide ratio, applies the price table, and returns the spend.
// Returns a zero breakdown when there are no classified tokens to apportion.
func EstimateCost(models []ModelStat, in, out, cacheRead, cacheWrite int64) CostBreakdown {
	var cb CostBreakdown
	classed := float64(in + out + cacheRead + cacheWrite)
	if classed <= 0 {
		return cb
	}
	fIn := float64(in) / classed
	fOut := float64(out) / classed
	fCR := float64(cacheRead) / classed
	fCW := float64(cacheWrite) / classed

	cb.AllPriced = len(models) > 0
	for _, m := range models {
		mc := ModelCost{ID: m.ID, Name: m.Name, Tokens: m.Tokens}
		cb.TotalToks += m.Tokens
		if p, ok := priceFor(m.ID); ok {
			t := float64(m.Tokens)
			mc.USD = (t*fIn*p.in + t*fOut*p.out + t*fCR*p.cacheRead + t*fCW*p.cacheWrite) / 1e6
			mc.Priced = true
			cb.Total += mc.USD
			cb.PricedToks += m.Tokens
			// What those cache-read tokens would have cost at the fresh-input
			// rate, minus what they actually cost — the saving from caching.
			cb.CacheSavingUSD += t * fCR * (p.in - p.cacheRead) / 1e6
		} else {
			cb.AllPriced = false
		}
		cb.Models = append(cb.Models, mc)
	}
	sort.Slice(cb.Models, func(i, j int) bool {
		if cb.Models[i].USD != cb.Models[j].USD {
			return cb.Models[i].USD > cb.Models[j].USD
		}
		return cb.Models[i].Tokens > cb.Models[j].Tokens
	})
	return cb
}

// FormatUSD renders a dollar amount at a sensible precision: tiny spends keep
// significant digits ($0.0042), normal spends show cents ($3.21), large spends
// round to whole dollars with separators ($1,240).
func FormatUSD(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	var s string
	switch {
	case v == 0:
		s = "$0.00"
	case v >= 1000:
		s = "$" + FormatInt(int(v+0.5))
	case v >= 1:
		s = fmt.Sprintf("$%.2f", v)
	case v >= 0.01:
		s = fmt.Sprintf("$%.3f", v)
	default:
		s = fmt.Sprintf("$%.4f", v)
	}
	if neg {
		return "-" + s
	}
	return s
}
