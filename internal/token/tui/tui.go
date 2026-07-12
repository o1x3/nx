// Package tui provides the interactive Bubble Tea front-end for nx. It
// reuses the same lipgloss card renderer as the static output, but lets you
// flip between harnesses, tabs and time ranges live.
package tui

import (
	"time"

	"github.com/o1x3/nx/internal/token/core"
	"github.com/o1x3/nx/internal/token/ui"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

var (
	harnesses = append([]string{core.Combined}, core.Harnesses...)
	ranges    = []string{core.RangeAll, core.Range30d, core.Range7d}
	tabs      = ui.TabOrder
)

// Options carries terminal state resolved by the CLI layer into the program.
type Options struct {
	Dark           bool // initial background guess from the CLI's detection
	DarkLocked     bool // NX_BACKGROUND set: never re-detect the background
	ForceTruecolor bool // NX_TRUECOLOR set: emit 24-bit colour regardless
}

type model struct {
	aggs    map[string]*core.Aggregate
	hi, ri  int // harness / range index
	tab     int
	now     time.Time
	w, h    int
	hintCol lipgloss.Style
	dark    bool // terminal background is dark
	plain   bool // terminal has no color support
	opts    Options
}

// New builds the interactive model with the given start harness/range/tab.
func New(harness, rng, tab string, opts Options) model {
	m := model{
		aggs: core.LoadEach(), // one pass over every harness, Combined included
		now:  time.Now(),
		dark: opts.Dark,
		opts: opts,
	}
	m.hi = indexOf(harnesses, harness, 0)
	m.ri = indexOf(ranges, rng, 0)
	m.tab = indexOf(tabs, tab, 0)
	m.hintCol = lipgloss.NewStyle().Foreground(lipgloss.Color("#565668"))
	ui.Configure(m.dark, m.plain)
	return m
}

func indexOf(s []string, v string, def int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return def
}

func (m model) Init() tea.Cmd {
	if m.opts.DarkLocked {
		return nil // NX_BACKGROUND override: don't ask the terminal
	}
	return tea.RequestBackgroundColor
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		if m.opts.DarkLocked {
			break
		}
		m.dark = msg.IsDark()
		ui.Configure(m.dark, m.plain)
	case tea.ColorProfileMsg:
		m.plain = msg.Profile <= colorprofile.ASCII
		ui.Configure(m.dark, m.plain)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "tab", "m":
			m.tab = (m.tab + 1) % len(tabs)
		case "shift+tab":
			m.tab = (m.tab - 1 + len(tabs)) % len(tabs)
		case "right", "l":
			m.hi = (m.hi + 1) % len(harnesses)
		case "left", "h":
			m.hi = (m.hi - 1 + len(harnesses)) % len(harnesses)
		case "1":
			m.ri = 0
		case "2":
			m.ri = 1
		case "3":
			m.ri = 2
		case "r":
			m.ri = (m.ri + 1) % len(ranges)
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	agg := m.aggs[harnesses[m.hi]]
	s := core.Summarize(agg, ranges[m.ri], m.now)
	card := ui.RenderCard(s, tabs[m.tab])

	hint := m.hintCol.Render("←/→ harness · tab/⇧tab views · 1/2/3 range · q quit")
	body := lipgloss.JoinVertical(lipgloss.Center, card, "", hint)

	if m.w > 0 && m.h > 0 {
		body = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, body)
	}
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

// Run starts the interactive program.
func Run(harness, rng, tab string, opts Options) error {
	var popts []tea.ProgramOption
	if opts.ForceTruecolor {
		popts = append(popts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	p := tea.NewProgram(New(harness, rng, tab, opts), popts...)
	_, err := p.Run()
	return err
}
