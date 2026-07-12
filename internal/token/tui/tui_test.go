package tui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/o1x3/nx/internal/token/core"
	"github.com/o1x3/nx/internal/token/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// fixedModel builds a model without touching the filesystem.
func fixedModel() model {
	m := model{aggs: map[string]*core.Aggregate{}, dark: true}
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

// key builds a KeyPressMsg for a printable character.
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestTUIViewRenders(t *testing.T) {
	m := fixedModel()
	m.w, m.h = 100, 40
	v := m.View()
	if !strings.Contains(v.Content, "Overview") {
		t.Error("TUI view should contain the Overview tab")
	}
	if !v.AltScreen {
		t.Error("TUI view should request the alternate screen")
	}
}

func TestTUIKeyNavigation(t *testing.T) {
	var m tea.Model = fixedModel()

	// tab cycles to Models
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mm := m.(model)
	if tabs[mm.tab] != ui.TabModels {
		t.Errorf("after tab, tab = %q, want models", tabs[mm.tab])
	}

	// shift+tab cycles back to Overview
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	mm = m.(model)
	if tabs[mm.tab] != ui.TabOverview {
		t.Errorf("after shift+tab, tab = %q, want overview", tabs[mm.tab])
	}

	// right cycles harness all -> claude
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	mm = m.(model)
	if harnesses[mm.hi] != core.Claude {
		t.Errorf("after right, harness = %q, want claude", harnesses[mm.hi])
	}

	// "2" selects the 30d range
	m, _ = m.Update(key('2'))
	mm = m.(model)
	if ranges[mm.ri] != core.Range30d {
		t.Errorf("after '2', range = %q, want 30d", ranges[mm.ri])
	}

	// q quits
	_, cmd := m.Update(key('q'))
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
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		mm := m.(model)
		if harnesses[mm.hi] != want {
			t.Errorf("after right, harness = %q, want %q", harnesses[mm.hi], want)
		}
	}

	// left wraps back from all -> cursor
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	mm := m.(model)
	if harnesses[mm.hi] != core.Cursor {
		t.Errorf("after left, harness = %q, want %q", harnesses[mm.hi], core.Cursor)
	}
}

// An NX_BACKGROUND override must survive interactive mode: no background
// query at startup, and terminal answers that do arrive are ignored.
func TestTUIDarkLocked(t *testing.T) {
	m := fixedModel()
	m.opts = Options{Dark: false, DarkLocked: true}
	m.dark = false

	if m.Init() != nil {
		t.Error("Init should not query the terminal when the background is locked")
	}
	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if mm := next.(model); mm.dark {
		t.Error("locked light background must ignore a dark BackgroundColorMsg")
	}
}

func TestTUIBackgroundDetection(t *testing.T) {
	m := fixedModel()

	// Init must ask the terminal for its background color.
	if m.Init() == nil {
		t.Error("Init should return the RequestBackgroundColor command")
	}

	// A light background flips dark off.
	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	mm := next.(model)
	if mm.dark {
		t.Error("white background should set dark = false")
	}

	// A dark background flips it back on.
	next, _ = mm.Update(tea.BackgroundColorMsg{Color: color.Black})
	mm = next.(model)
	if !mm.dark {
		t.Error("black background should set dark = true")
	}

	// An ASCII color profile marks the terminal as plain.
	next, _ = mm.Update(tea.ColorProfileMsg{Profile: colorprofile.ASCII})
	mm = next.(model)
	if !mm.plain {
		t.Error("ASCII profile should set plain = true")
	}
	next, _ = mm.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	mm = next.(model)
	if mm.plain {
		t.Error("TrueColor profile should set plain = false")
	}
}
