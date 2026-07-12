package core

import (
	"testing"
	"time"
)

// aggWithHours builds an aggregate with hour histograms on given days.
func aggWithHours() *Aggregate {
	a := newAggregate(Combined)
	// 2026-06-29 is a Monday; 2026-06-28 a Sunday.
	a.ByDayMsgs["2026-06-29"] = 5
	a.ByDayTokens["2026-06-29"] = 900
	a.ByDayHour["2026-06-29"] = &[24]int{9: 2, 14: 3}
	a.ByDayMsgs["2026-06-28"] = 4
	a.ByDayTokens["2026-06-28"] = 1500
	a.ByDayHour["2026-06-28"] = &[24]int{14: 4}
	a.ByDayMsgs["2026-06-01"] = 2 // outside a 7d window from 06-29
	a.ByDayTokens["2026-06-01"] = 300
	a.ByDayHour["2026-06-01"] = &[24]int{23: 2}
	return a
}

func TestHoursIn(t *testing.T) {
	a := aggWithHours()
	all := a.HoursIn(allDays)
	if all[14] != 7 {
		t.Errorf("hour 14 = %d, want 7", all[14])
	}
	if all[9] != 2 || all[23] != 2 {
		t.Errorf("hours 9/23 = %d/%d, want 2/2", all[9], all[23])
	}
	if a.PeakHourIn(allDays) != 14 {
		t.Errorf("peak hour = %d, want 14", a.PeakHourIn(allDays))
	}
}

func TestWeekdayBreakdown(t *testing.T) {
	a := aggWithHours()
	wd := a.WeekdayMsgsIn(allDays)
	// 06-29 (5 msgs) and 06-01 (2 msgs) are both Mondays → 7; 06-28 is Sunday (4).
	if wd[time.Monday] != 7 {
		t.Errorf("Monday msgs = %d, want 7", wd[time.Monday])
	}
	if wd[time.Sunday] != 4 {
		t.Errorf("Sunday msgs = %d, want 4", wd[time.Sunday])
	}
	tk := a.WeekdayTokensIn(allDays)
	if tk[time.Sunday] != 1500 {
		t.Errorf("Sunday tokens = %d, want 1500", tk[time.Sunday])
	}
}

func TestPunchcardIn(t *testing.T) {
	a := aggWithHours()
	grid := a.PunchcardIn(allDays)
	if grid[time.Monday][14] != 3 {
		t.Errorf("Mon@14 = %d, want 3", grid[time.Monday][14])
	}
	if grid[time.Sunday][14] != 4 {
		t.Errorf("Sun@14 = %d, want 4", grid[time.Sunday][14])
	}
	if grid[time.Monday][9] != 2 {
		t.Errorf("Mon@9 = %d, want 2", grid[time.Monday][9])
	}
}

func TestTopDaysIn(t *testing.T) {
	a := aggWithHours()
	top := a.TopDaysIn(allDays, 2)
	if len(top) != 2 {
		t.Fatalf("got %d days, want 2", len(top))
	}
	if top[0].Day != "2026-06-28" || top[0].Tokens != 1500 {
		t.Errorf("busiest = %s/%d, want 2026-06-28/1500", top[0].Day, top[0].Tokens)
	}
	if top[1].Day != "2026-06-29" {
		t.Errorf("second = %s, want 2026-06-29", top[1].Day)
	}
}

func TestDailySeries(t *testing.T) {
	a := aggWithHours()
	end := time.Date(2026, 6, 29, 12, 0, 0, 0, time.Local)
	s := a.DailySeries(end, 3) // 06-27, 06-28, 06-29 oldest first
	if len(s) != 3 {
		t.Fatalf("len = %d, want 3", len(s))
	}
	if s[0] != 0 {
		t.Errorf("06-27 (no data) = %d, want 0", s[0])
	}
	if s[1] != 1500 || s[2] != 900 {
		t.Errorf("series tail = %d,%d, want 1500,900", s[1], s[2])
	}
}

func TestPriceFor(t *testing.T) {
	cases := []struct {
		id     string
		wantIn float64
		wantOk bool
	}{
		{"claude-opus-4-8", 15, true},
		{"anthropic/claude-sonnet-4-6", 3, true},
		{"gpt-5.4", 2.5, true},
		{"gpt-5.4-nano", 0.2, true},
		{"gpt-5.4-mini", 0.75, true},
		{"gpt-5", 1.25, true},
		{"gpt-5-codex", 1.25, true}, // codex stripped → base gpt-5
		{"mystery-model-x", 0, false},
	}
	for _, c := range cases {
		p, ok := priceFor(c.id)
		if ok != c.wantOk {
			t.Errorf("priceFor(%q) ok = %v, want %v", c.id, ok, c.wantOk)
			continue
		}
		if ok && p.in != c.wantIn {
			t.Errorf("priceFor(%q).in = %g, want %g", c.id, p.in, c.wantIn)
		}
	}
}

func TestEstimateCost(t *testing.T) {
	// 1M tokens of one model, all input, opus pricing → $15.
	models := []ModelStat{{ID: "claude-opus-4-8", Name: "Opus 4.8", Tokens: 1_000_000}}
	cb := EstimateCost(models, 1_000_000, 0, 0, 0)
	if !cb.AllPriced {
		t.Error("expected all priced")
	}
	if cb.Total < 14.9 || cb.Total > 15.1 {
		t.Errorf("total = %.4f, want ~15", cb.Total)
	}

	// Unknown model is unpriced but still listed.
	cb2 := EstimateCost([]ModelStat{{ID: "who-knows", Tokens: 1000}}, 1000, 0, 0, 0)
	if cb2.AllPriced {
		t.Error("unknown model should make AllPriced false")
	}
	if cb2.Total != 0 {
		t.Errorf("unknown model total = %.4f, want 0", cb2.Total)
	}

	// No classified tokens → zero breakdown, no panic.
	if cb3 := EstimateCost(models, 0, 0, 0, 0); cb3.Total != 0 || cb3.Models != nil {
		t.Errorf("empty classed total = %.4f, models = %v", cb3.Total, cb3.Models)
	}
}

func TestFormatUSD(t *testing.T) {
	cases := map[float64]string{
		0:      "$0.00",
		0.0042: "$0.0042",
		0.42:   "$0.420",
		3.215:  "$3.21",
		1240.7: "$1,241",
		-5.5:   "-$5.50",
	}
	for in, want := range cases {
		if got := FormatUSD(in); got != want {
			t.Errorf("FormatUSD(%g) = %q, want %q", in, got, want)
		}
	}
}
