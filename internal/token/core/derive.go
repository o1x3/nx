package core

import (
	"sort"
	"time"
)

// parseDay turns a civil-day key (YYYY-MM-DD) into a local midnight time. The
// bool is false when the key is malformed.
func parseDay(d string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", d, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// HoursIn returns the 0-23 message histogram across the civil days kept by the
// window predicate.
func (a *Aggregate) HoursIn(keep func(string) bool) [24]int {
	var hours [24]int
	for d, h := range a.ByDayHour {
		if keep(d) {
			for i, v := range h {
				hours[i] += v
			}
		}
	}
	return hours
}

// WeekdayMsgsIn returns message counts indexed by weekday (0=Sunday .. 6=Saturday)
// within the window.
func (a *Aggregate) WeekdayMsgsIn(keep func(string) bool) [7]int {
	var wd [7]int
	for d, n := range a.ByDayMsgs {
		if keep(d) {
			if t, ok := parseDay(d); ok {
				wd[int(t.Weekday())] += n
			}
		}
	}
	return wd
}

// WeekdayTokensIn returns token sums indexed by weekday within the window.
func (a *Aggregate) WeekdayTokensIn(keep func(string) bool) [7]int64 {
	var wd [7]int64
	for d, v := range a.ByDayTokens {
		if keep(d) {
			if t, ok := parseDay(d); ok {
				wd[int(t.Weekday())] += v
			}
		}
	}
	return wd
}

// PunchcardIn returns a [weekday][hour] message-count grid within the window —
// the classic GitHub "punch card" of when work happens.
func (a *Aggregate) PunchcardIn(keep func(string) bool) [7][24]int {
	var grid [7][24]int
	for d, h := range a.ByDayHour {
		if keep(d) {
			if t, ok := parseDay(d); ok {
				wd := int(t.Weekday())
				for i, v := range h {
					grid[wd][i] += v
				}
			}
		}
	}
	return grid
}

// DayStat is one civil day's rolled-up activity.
type DayStat struct {
	Date     time.Time
	Day      string
	Tokens   int64
	Messages int
}

// TopDaysIn returns the busiest civil days within the window, ranked by tokens
// (messages break ties), truncated to n. Days with messages but no recorded
// tokens are included so a high-activity, low-token day still surfaces.
func (a *Aggregate) TopDaysIn(keep func(string) bool, n int) []DayStat {
	seen := map[string]bool{}
	out := make([]DayStat, 0, len(a.ByDayTokens))
	add := func(d string) {
		if seen[d] || !keep(d) {
			return
		}
		t, ok := parseDay(d)
		if !ok {
			return
		}
		seen[d] = true
		out = append(out, DayStat{Date: t, Day: d, Tokens: a.ByDayTokens[d], Messages: a.ByDayMsgs[d]})
	}
	for d := range a.ByDayTokens {
		add(d)
	}
	for d := range a.ByDayMsgs {
		add(d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Day > out[j].Day // newest first on a full tie
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// DailySeries returns the token total for each of the n civil days ending at
// end (inclusive), oldest first — a contiguous run suitable for a sparkline.
func (a *Aggregate) DailySeries(end time.Time, n int) []int64 {
	if n <= 0 {
		return nil
	}
	day := civil(end)
	out := make([]int64, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = a.ByDayTokens[day.Format("2006-01-02")]
		day = day.AddDate(0, 0, -1)
	}
	return out
}
