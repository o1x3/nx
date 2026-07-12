package core

import "time"

// SummaryJSON is the machine-readable form of a Summary. It is a deliberately
// flat, snake_case DTO with a schema version — a stable scripting/CI contract,
// independent of the internal Summary layout (which has no json tags and embeds
// the heatmap grid). The heatmap is intentionally omitted.
type SummaryJSON struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Harness       string    `json:"harness"`
	Range         string    `json:"range"`

	Tokens struct {
		Total      int64 `json:"total"`
		Input      int64 `json:"input"`
		Output     int64 `json:"output"`
		CacheRead  int64 `json:"cache_read"`
		CacheWrite int64 `json:"cache_write"`
	} `json:"tokens"`
	// TokenSplitExact is true only for the all-time range, where the input/
	// output/cache figures are the authoritative ledger totals. For windowed
	// ranges the split is apportioned proportionally — an estimate, not a count.
	TokenSplitExact bool `json:"token_split_exact"`
	// TokensEstimated is true when part of the token totals were estimated
	// from text length (Cursor doesn't record usage) rather than counted.
	TokensEstimated bool `json:"tokens_estimated"`

	Sessions   int `json:"sessions"`
	Messages   int `json:"messages"`
	ActiveDays int `json:"active_days"`
	Streak     struct {
		Current int `json:"current"`
		Longest int `json:"longest"`
	} `json:"streak"`
	PeakHour int    `json:"peak_hour"` // 0..23, or -1 when idle
	FavModel string `json:"fav_model"`

	Models []ModelJSON `json:"models"`

	EstUSD          float64 `json:"est_usd"`
	EstUSDAllPriced bool    `json:"est_usd_all_priced"` // false ⇒ some model had no price
	HobbitFactor    float64 `json:"hobbit_factor"`
}

// ModelJSON is one model's line in the JSON output.
type ModelJSON struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Tokens   int64  `json:"tokens"`
	Messages int    `json:"messages"`
}

// NewSummaryJSON projects a Summary onto the stable JSON DTO.
func NewSummaryJSON(s Summary, now time.Time) SummaryJSON {
	j := SummaryJSON{
		SchemaVersion:   1,
		GeneratedAt:     now,
		Harness:         s.Harness,
		Range:           s.Range,
		TokenSplitExact: s.Range == RangeAll,
		TokensEstimated: s.TokensEstimated,
		Sessions:        s.Sessions,
		Messages:        s.Messages,
		ActiveDays:      s.ActiveDays,
		PeakHour:        s.PeakHour,
		FavModel:        s.FavModel,
		EstUSD:          s.Cost.Total,
		EstUSDAllPriced: s.Cost.AllPriced,
		HobbitFactor:    s.HobbitFactor,
	}
	j.Tokens.Total = s.TotalTokens
	j.Tokens.Input = s.InputTokens
	j.Tokens.Output = s.OutputTokens
	j.Tokens.CacheRead = s.CacheReadTokens
	j.Tokens.CacheWrite = s.CacheWriteTokens
	j.Streak.Current = s.CurrentStreak
	j.Streak.Longest = s.LongestStreak
	for _, m := range s.Models {
		j.Models = append(j.Models, ModelJSON{Name: m.Name, ID: m.ID, Tokens: m.Tokens, Messages: m.Messages})
	}
	return j
}

// HasData reports whether a summary saw any activity (used for exit codes in
// the standalone output modes).
func (s Summary) HasData() bool { return s.Messages > 0 || s.Sessions > 0 || s.TotalTokens > 0 }
