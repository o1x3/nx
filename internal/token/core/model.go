// Package core loads usage data from the Claude Code, Codex, pi.dev and
// Cursor coding harnesses and aggregates it into the stats nx renders.
package core

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Harness identifiers.
const (
	Claude   = "claude"
	Codex    = "codex"
	Pi       = "pi"
	Cursor   = "cursor"
	Combined = "all"
)

// Harnesses lists every concrete harness in canonical order (Combined is the
// merge of these, not a member).
var Harnesses = []string{Claude, Codex, Pi, Cursor}

// HarnessTitle is the human label for a harness key.
func HarnessTitle(h string) string {
	switch h {
	case Claude:
		return "Claude Code"
	case Codex:
		return "Codex"
	case Pi:
		return "pi.dev"
	case Cursor:
		return "Cursor"
	default:
		return "All harnesses"
	}
}

// Aggregate is the raw rollup of one harness (or the merge of several).
type Aggregate struct {
	Harness          string
	Sessions         int
	Messages         int
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64

	ByDayTokens map[string]int64 // civil date (YYYY-MM-DD, local) -> tokens
	ByDayMsgs   map[string]int   // civil date -> message count (defines active days)

	// per-civil-day breakdowns, so peak hour / model share can be windowed
	ByDayHour     map[string]*[24]int         // day -> hour histogram
	ByDayModelTok map[string]map[string]int64 // day -> model -> tokens
	ByDayModelMsg map[string]map[string]int   // day -> model -> messages

	// TokensEstimated is true when any of the token totals were derived from
	// text length (or a local Cursor meter snapshot) rather than a billed ledger.
	TokensEstimated bool

	First time.Time
	Last  time.Time
}

func newAggregate(h string) *Aggregate {
	return &Aggregate{
		Harness:       h,
		ByDayTokens:   map[string]int64{},
		ByDayMsgs:     map[string]int{},
		ByDayHour:     map[string]*[24]int{},
		ByDayModelTok: map[string]map[string]int64{},
		ByDayModelMsg: map[string]map[string]int{},
	}
}

// TotalTokens is every token that flowed through the model: fresh input,
// output, cache reads and cache writes.
func (a *Aggregate) TotalTokens() int64 {
	return a.InputTokens + a.OutputTokens + a.CacheReadTokens + a.CacheWriteTokens
}

// noteMessage records a message at time t (local) under model m, adding tok
// tokens to that day. Pass tok=0 for a message with no usage of its own.
func (a *Aggregate) noteMessage(t time.Time, tok int64) {
	if t.IsZero() {
		return
	}
	lt := t.Local()
	day := lt.Format("2006-01-02")
	a.Messages++
	a.ByDayMsgs[day]++
	a.dayHour(day)[lt.Hour()]++
	if tok > 0 {
		a.ByDayTokens[day] += tok
	}
	if a.First.IsZero() || lt.Before(a.First) {
		a.First = lt
	}
	if lt.After(a.Last) {
		a.Last = lt
	}
}

// addTokensOnDay attributes tokens to a civil day without counting a message
// (used by Codex, whose token totals are session-level).
func (a *Aggregate) addTokensOnDay(t time.Time, tok int64) {
	if tok <= 0 || t.IsZero() {
		return
	}
	a.ByDayTokens[t.Local().Format("2006-01-02")] += tok
}

func (a *Aggregate) dayHour(day string) *[24]int {
	h := a.ByDayHour[day]
	if h == nil {
		h = &[24]int{}
		a.ByDayHour[day] = h
	}
	return h
}

// addModelOnDay records model usage attributed to a civil day, keeping both
// the all-time and per-day breakdowns in sync.
func (a *Aggregate) addModelOnDay(day, m string, tok int64) {
	if m == "" || day == "" {
		return
	}
	if a.ByDayModelMsg[day] == nil {
		a.ByDayModelMsg[day] = map[string]int{}
	}
	a.ByDayModelMsg[day][m]++
	if tok > 0 {
		if a.ByDayModelTok[day] == nil {
			a.ByDayModelTok[day] = map[string]int64{}
		}
		a.ByDayModelTok[day][m] += tok
	}
}

// addModelTokensOnDay attributes only tokens (no message count) to a model on
// a civil day. Used by Codex, whose token deltas aren't per-message.
func (a *Aggregate) addModelTokensOnDay(day, m string, tok int64) {
	if m == "" || day == "" || tok <= 0 {
		return
	}
	if a.ByDayModelTok[day] == nil {
		a.ByDayModelTok[day] = map[string]int64{}
	}
	a.ByDayModelTok[day][m] += tok
}

// dayOf returns the civil-day key for a timestamp (empty if zero).
func dayOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

// Merge folds o into a, producing a combined aggregate.
func (a *Aggregate) Merge(o *Aggregate) {
	a.Sessions += o.Sessions
	a.Messages += o.Messages
	a.InputTokens += o.InputTokens
	a.OutputTokens += o.OutputTokens
	a.CacheReadTokens += o.CacheReadTokens
	a.CacheWriteTokens += o.CacheWriteTokens
	// The estimation taint only spreads when the estimated source actually
	// contributed tokens; an idle estimated harness doesn't taint the merge.
	if o.TokensEstimated && o.TotalTokens() > 0 {
		a.TokensEstimated = true
	}
	for d, v := range o.ByDayTokens {
		a.ByDayTokens[d] += v
	}
	for d, v := range o.ByDayMsgs {
		a.ByDayMsgs[d] += v
	}
	for d, h := range o.ByDayHour {
		dst := a.dayHour(d)
		for i, v := range h {
			dst[i] += v
		}
	}
	for d, mm := range o.ByDayModelTok {
		if a.ByDayModelTok[d] == nil {
			a.ByDayModelTok[d] = map[string]int64{}
		}
		for m, v := range mm {
			a.ByDayModelTok[d][m] += v
		}
	}
	for d, mm := range o.ByDayModelMsg {
		if a.ByDayModelMsg[d] == nil {
			a.ByDayModelMsg[d] = map[string]int{}
		}
		for m, v := range mm {
			a.ByDayModelMsg[d][m] += v
		}
	}
	if !o.First.IsZero() && (a.First.IsZero() || o.First.Before(a.First)) {
		a.First = o.First
	}
	if o.Last.After(a.Last) {
		a.Last = o.Last
	}
}

// ModelStat is one model's share of usage.
type ModelStat struct {
	ID       string
	Name     string
	Tokens   int64
	Messages int
}

// allDays accepts every civil day (used for the all-time window).
func allDays(string) bool { return true }

// TopModelsIn returns models sorted by tokens descending, restricted to the
// civil days for which keep(day) is true.
func (a *Aggregate) TopModelsIn(keep func(string) bool) []ModelStat {
	tok := map[string]int64{}
	msg := map[string]int{}
	for d, mm := range a.ByDayModelTok {
		if keep(d) {
			for m, v := range mm {
				tok[m] += v
			}
		}
	}
	for d, mm := range a.ByDayModelMsg {
		if keep(d) {
			for m, v := range mm {
				msg[m] += v
			}
		}
	}
	out := make([]ModelStat, 0, len(msg))
	seen := map[string]bool{}
	for id, t := range tok {
		out = append(out, ModelStat{ID: id, Name: FriendlyModel(id), Tokens: t, Messages: msg[id]})
		seen[id] = true
	}
	for id, m := range msg {
		if !seen[id] {
			out = append(out, ModelStat{ID: id, Name: FriendlyModel(id), Messages: m})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Messages > out[j].Messages
	})
	return out
}

// TopModels returns all-time models sorted by tokens descending.
func (a *Aggregate) TopModels() []ModelStat { return a.TopModelsIn(allDays) }

// FavoriteModelIn is the most-used model by tokens within the window.
func (a *Aggregate) FavoriteModelIn(keep func(string) bool) string {
	m := a.TopModelsIn(keep)
	if len(m) == 0 {
		return "—"
	}
	return m[0].Name
}

// FavoriteModel is the all-time most-used model by tokens.
func (a *Aggregate) FavoriteModel() string { return a.FavoriteModelIn(allDays) }

// PeakHourIn returns the local hour (0-23) with the most messages within the
// window, or -1 when there is no activity.
func (a *Aggregate) PeakHourIn(keep func(string) bool) int {
	hours := a.HoursIn(keep)
	best, bestN := -1, 0
	for h, n := range hours {
		if n > bestN {
			best, bestN = h, n
		}
	}
	return best
}

// PeakHour returns the all-time peak local hour, or -1.
func (a *Aggregate) PeakHour() int { return a.PeakHourIn(allDays) }

// ActiveDays is the count of distinct local days with at least one message.
func (a *Aggregate) ActiveDays() int {
	n := 0
	for _, v := range a.ByDayMsgs {
		if v > 0 {
			n++
		}
	}
	return n
}

// activeDaySet returns the set of active civil days as date values.
func (a *Aggregate) activeDaySet() map[string]bool {
	s := make(map[string]bool, len(a.ByDayMsgs))
	for d, v := range a.ByDayMsgs {
		if v > 0 {
			s[d] = true
		}
	}
	return s
}

// Streaks returns (current, longest) consecutive-active-day streaks relative
// to now. The current streak counts back from today (or yesterday, if today
// has no activity yet).
func (a *Aggregate) Streaks(now time.Time) (current, longest int) {
	set := a.activeDaySet()
	if len(set) == 0 {
		return 0, 0
	}
	days := make([]time.Time, 0, len(set))
	for d := range set {
		t, err := time.ParseInLocation("2006-01-02", d, time.Local)
		if err == nil {
			days = append(days, t)
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	run := 1
	longest = 1
	for i := 1; i < len(days); i++ {
		if days[i].Equal(days[i-1].AddDate(0, 0, 1)) { // calendar-aware, DST-safe
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 1
		}
	}

	today := civil(now)
	cur := today
	if !set[cur.Format("2006-01-02")] {
		cur = cur.AddDate(0, 0, -1)
		if !set[cur.Format("2006-01-02")] {
			return 0, longest
		}
	}
	for set[cur.Format("2006-01-02")] {
		current++
		cur = cur.AddDate(0, 0, -1)
	}
	return current, longest
}

// civil returns t truncated to local midnight.
func civil(t time.Time) time.Time {
	lt := t.Local()
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.Local)
}

// FriendlyModel maps a raw model id to a display name.
func FriendlyModel(id string) string {
	if id == "" {
		return "—"
	}
	raw := id
	// strip provider prefixes and date suffixes
	id = strings.TrimPrefix(id, "anthropic/")
	id = strings.TrimPrefix(id, "openai/")
	low := strings.ToLower(id)

	if low == "auto" || low == "default" {
		return "Auto"
	}

	codex := strings.Contains(low, "codex")
	cleanDate := func(s string) string {
		// drop trailing -YYYYMMDD or [1m] markers
		s = strings.TrimSuffix(s, "[1m]")
		if i := strings.LastIndex(s, "-202"); i > 0 {
			s = s[:i]
		}
		return s
	}

	switch {
	case strings.HasPrefix(low, "claude-"):
		rest := cleanDate(low[len("claude-"):])
		rest = strings.TrimSuffix(rest, "[1m]")
		parts := strings.Split(rest, "-")
		// Cursor-style reversed ids put the version first (claude-4.5-sonnet);
		// flip them back to "Sonnet 4.5".
		if len(parts) >= 2 && digitStart(parts[0]) && !digitStart(parts[len(parts)-1]) {
			fam := titleCase(parts[len(parts)-1])
			ver := strings.Join(parts[:len(parts)-1], ".")
			return fam + " " + ver
		}
		if len(parts) >= 1 {
			fam := titleCase(parts[0]) // opus / sonnet / haiku / fable
			ver := strings.Join(parts[1:], ".")
			ver = strings.TrimSuffix(ver, ".")
			if ver == "" {
				return fam
			}
			return fam + " " + ver
		}
	case strings.HasPrefix(low, "gpt-"):
		rest := cleanDate(low)
		rest = strings.ReplaceAll(rest, "gpt-", "GPT-")
		rest = strings.ReplaceAll(rest, "-codex", " Codex")
		return rest
	case strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4"):
		return strings.ToUpper(cleanDate(low))
	case strings.HasPrefix(low, "gemini"):
		return titleCase(strings.ReplaceAll(cleanDate(low), "-", " "))
	}
	if codex && !strings.Contains(strings.ToLower(raw), "codex-") {
		return titleCase(raw)
	}
	return raw
}

func digitStart(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
