package tui

import (
	"strings"
	"testing"

	"github.com/o1x3/nx/internal/token/core"
	"github.com/o1x3/nx/internal/token/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// fixedModel builds a model without touching the filesystem.
func fixedModel() model {
	m := model{aggs: map[string]*core.Aggregate{}}
	for _, h := range harnesses {
		a := &core.Aggregate{
			Harness:       h,
			Sessions:      10,
			Messages:      100,
			InputTokens:   1_000_000,
			ByDayTokens:   map[string]int64{"2026-06-29": 1000},
			ByDayMsgs:     map[string]int{"2026-06-29": 10},
			ByDayHour:     map[string]*[24]int{"2026-06-29": {12: 10}},
			ByDayModelTok: map[string]map[string]int64{"2026-06-29": {"claude-opus-4-8": 1_000_000}},
			ByDayModelMsg: map[string]map[string]int{"2026-06-29": {"claude-opus-4-8": 10}},
		}
		m.aggs[h] = a
	}
	return m
}

func TestTUIViewRenders(t *testing.T) {
	m := fixedModel()
	m.w, m.h = 100, 40
	if !strings.Contains(m.View(), "Overview") {
		t.Error("TUI view should contain the Overview tab")
	}
}

func TestTUIKeyNavigation(t *testing.T) {
	var m tea.Model = fixedModel()

	// tab cycles to Models
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
	mm := m.(model)
	if tabs[mm.tab] != ui.TabModels {
		t.Errorf("after tab, tab = %q, want models", tabs[mm.tab])
	}

	// right cycles harness all -> claude
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	mm = m.(model)
	if harnesses[mm.hi] != core.Claude {
		t.Errorf("after right, harness = %q, want claude", harnesses[mm.hi])
	}

	// "2" selects the 30d range
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	mm = m.(model)
	if ranges[mm.ri] != core.Range30d {
		t.Errorf("after '2', range = %q, want 30d", ranges[mm.ri])
	}

	// q quits
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestTUIHarnessCycleReachesNewHarnesses(t *testing.T) {
	start := fixedModel()
	start.hi = indexOf(harnesses, core.Pi, 0)
	var m tea.Model = start

	// right cycles pi -> cursor -> wraps to all
	for _, want := range []string{core.Cursor, core.Combined} {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		mm := m.(model)
		if harnesses[mm.hi] != want {
			t.Errorf("after right, harness = %q, want %q", harnesses[mm.hi], want)
		}
	}

	// left wraps back from all -> cursor
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	mm := m.(model)
	if harnesses[mm.hi] != core.Cursor {
		t.Errorf("after left, harness = %q, want %q", harnesses[mm.hi], core.Cursor)
	}
}
