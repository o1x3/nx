package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/o1x3/nx/internal/token/core"
	"github.com/o1x3/nx/internal/token/tui"
	"github.com/o1x3/nx/internal/token/ui"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

type tokenOptions struct {
	harness     string
	rng         string
	tab         string
	interactive bool
	jsonOut     bool
	quiet       bool
	compare     bool
	help        bool
}

// parseTokenArgs ports tmax's positional, order-independent argument grammar.
// Bare "all" is a harness keyword; the all-time range keywords are "alltime"
// and "lifetime" (all-time is also the default).
func parseTokenArgs(args []string) (tokenOptions, error) {
	o := tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview}
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			o.help = true
			return o, nil
		case "-i", "--interactive", "-t", "--tui", "tui":
			o.interactive = true

		// ---- harnesses ----
		case "claude", "cc", "claude-code":
			o.harness = core.Claude
		case "codex", "cx":
			o.harness = core.Codex
		case "pi", "pi.dev", "pidev":
			o.harness = core.Pi
		case "cursor", "cursor-ide", "cursor-cli", "cursor-agent":
			o.harness = core.Cursor
		case "all", "combined", "everything":
			o.harness = core.Combined

		// ---- ranges ----
		case "30d", "month", "30":
			o.rng = core.Range30d
		case "7d", "week", "7":
			o.rng = core.Range7d
		case "alltime", "lifetime":
			o.rng = core.RangeAll

		// ---- body tabs ----
		case "overview", "-o", "--overview":
			o.tab = ui.TabOverview
		case "models", "model", "-m", "--models":
			o.tab = ui.TabModels
		case "hours", "clock", "--hours":
			o.tab = ui.TabHours
		case "punchcard", "punch", "when", "--punchcard":
			o.tab = ui.TabPunchcard
		case "trend", "spark", "series", "--trend":
			o.tab = ui.TabTrend
		case "topdays", "busiest", "--topdays":
			o.tab = ui.TabTopDays
		case "weekday", "dow", "--weekday":
			o.tab = ui.TabWeekday
		case "cost", "spend", "--cost":
			o.tab = ui.TabCost
		case "mix", "split", "--mix":
			o.tab = ui.TabMix

		// ---- standalone output modes (bypass the card) ----
		case "json", "--json", "--stats":
			o.jsonOut = true
		case "quiet", "-q", "--quiet":
			o.quiet = true
		case "compare", "vs", "--compare":
			o.compare = true

		default:
			return o, ExitError{Code: 2, Err: fmt.Errorf("unknown argument %q (try: nx help token)", a)}
		}
	}
	return o, nil
}

func (a App) runToken(ctx context.Context, args []string, stdout io.Writer) error {
	_ = ctx

	o, err := parseTokenArgs(args)
	if err != nil {
		return err
	}
	if o.help {
		fmt.Fprintln(stdout, tokenHelpText())
		return nil
	}

	forced := os.Getenv("NX_TRUECOLOR") != ""
	tty := term.IsTerminal(os.Stdout.Fd())

	now := time.Now()

	// quiet/json bypass the card and emit no styling, so dispatch them before
	// any colour work — no reason to query the terminal for those. They exit 3
	// when the selected harness has no usage, so they compose in scripts and CI.
	switch {
	case o.quiet:
		return runTokenQuiet(o, now, stdout)
	case o.jsonOut:
		return runTokenJSON(o, now, stdout)
	}

	// Light/dark detection so foreground colours stay legible on any terminal.
	// nx token paints no background; it adapts to yours. NX_BACKGROUND overrides
	// (and skips the terminal query, which momentarily raw-modes the tty).
	// Non-TTY output defaults to the dark palette, matching v1 behaviour.
	dark := true
	darkLocked := false
	switch os.Getenv("NX_BACKGROUND") {
	case "light":
		dark, darkLocked = false, true
	case "dark":
		dark, darkLocked = true, true
	default:
		if tty {
			dark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
		}
	}

	// Colour handling: lipgloss v2 styles always emit truecolor escapes;
	// downsampling/stripping happens at write time. Force 24-bit when asked
	// (capture / under-reporting terminals), strip every escape when piped or
	// redirected, otherwise downsample to whatever the terminal supports.
	var profile colorprofile.Profile
	switch {
	case forced:
		profile = colorprofile.TrueColor
	case !tty:
		profile = colorprofile.NoTTY
	default:
		profile = colorprofile.Detect(os.Stdout, os.Environ())
	}

	// Plain mode follows the effective profile, not just tty-ness, so NO_COLOR
	// or TERM=dumb on a real terminal still gets shade-glyph density cells.
	ui.Configure(dark, profile <= colorprofile.ASCII)
	out := &colorprofile.Writer{Forward: stdout, Profile: profile}

	if o.compare {
		return runTokenCompare(o, now, out)
	}

	if o.interactive {
		return tui.Run(o.harness, o.rng, o.tab, tui.Options{
			Dark:           dark,
			DarkLocked:     darkLocked,
			ForceTruecolor: forced,
		})
	}

	agg := core.Load(o.harness)
	s := core.Summarize(agg, o.rng, now)
	fmt.Fprintln(out, ui.RenderCard(s, o.tab))

	// Nudge toward the interactive view when on a real terminal. Update checks
	// are nx-wide (selfupdate), not per-command, so no notice is printed here.
	if tty {
		fmt.Fprintln(out, ui.Hint("run `nx token -i` for the interactive view"))
	}
	return nil
}

// tokenTag renders the "[harness·range]" provenance tag for quiet mode,
// dropping it only for the bare default (all harnesses, all time).
func tokenTag(o tokenOptions) string {
	if o.harness == core.Combined && o.rng == core.RangeAll {
		return ""
	}
	h := o.harness
	if h == core.Combined {
		h = "all"
	}
	return "[" + h + "·" + o.rng + "]"
}

// runTokenQuiet prints exactly one terse, prompt-safe line of headline numbers.
// No colour, no hint, single newline. Exit 3 when the selection has no usage.
func runTokenQuiet(o tokenOptions, now time.Time, stdout io.Writer) error {
	s := core.Summarize(core.Load(o.harness), o.rng, now)
	if !s.HasData() {
		fmt.Fprintln(stdout, "nx token: no usage for this selection")
		return ExitError{Code: 3}
	}
	fmt.Fprintf(stdout, "nx token%s %s tok · %s msgs · %dd streak · %s\n",
		tokenTag(o), core.FormatTokens(s.TotalTokens), core.FormatInt(s.Messages),
		s.CurrentStreak, s.FavModel)
	return nil
}

// runTokenJSON emits the machine-readable summary: one indented object for a
// single harness, or NDJSON (one compact object per concrete harness) for
// "all", so `nx token all json | jq -s` works.
func runTokenJSON(o tokenOptions, now time.Time, stdout io.Writer) error {
	enc := json.NewEncoder(stdout)
	if o.harness == core.Combined {
		any := false
		for _, h := range core.Harnesses {
			s := core.Summarize(core.Load(h), o.rng, now)
			_ = enc.Encode(core.NewSummaryJSON(s, now))
			any = any || s.HasData()
		}
		if !any {
			return ExitError{Code: 3}
		}
		return nil
	}
	s := core.Summarize(core.Load(o.harness), o.rng, now)
	enc.SetIndent("", "  ")
	_ = enc.Encode(core.NewSummaryJSON(s, now))
	if !s.HasData() {
		return ExitError{Code: 3}
	}
	return nil
}

// runTokenCompare renders the side-by-side harness card. stdout is the card
// path's colorprofile writer, so piped output is stripped of escapes and
// lesser terminals get downsampled colours.
func runTokenCompare(o tokenOptions, now time.Time, stdout io.Writer) error {
	var sums []core.Summary
	for _, h := range core.Harnesses {
		if s := core.Summarize(core.Load(h), o.rng, now); s.HasData() {
			sums = append(sums, s)
		}
	}
	fmt.Fprintln(stdout, ui.RenderCompare(sums, o.rng))
	if len(sums) == 0 {
		return ExitError{Code: 3}
	}
	return nil
}

func tokenHelpText() string {
	return `nx token — token stats across your AI coding harnesses

USAGE
  nx token [harness] [range] [tab] [-i]
  nx token [harness] [range] (json | quiet | compare)
  nx help token [topic]

HARNESS   (default: all)
  claude            Claude Code        ~/.claude + ~/.config/claude
  codex             OpenAI Codex       ~/.codex (sessions + archived)
  pi                pi.dev             ~/.pi/agent/sessions
  cursor            Cursor IDE + CLI   state.vscdb + ~/.cursor
  all               every harness merged

  Overrides: CLAUDE_CONFIG_DIR, CODEX_HOME, PI_AGENT_DIR.
  Claude uses final streaming chunks; Cursor prefers real counts / composer
  meter, else ~4 bytes/token. Cursor Auto resolves underlying models locally
  when available (local totals undercount the admin dashboard).

RANGE     (default: alltime)
  alltime           lifetime
  30d               last 30 days
  7d                last 7 days

TAB       (default: overview)
  overview          headline stats + activity heatmap
  models            token share by model
  hours             activity by local hour (clock)
  punchcard         weekday × hour density grid (when)
  trend             daily-token sparkline + averages + momentum (spark)
  topdays           your busiest days, ranked (busiest)
  weekday           tokens by day of week (dow)
  cost              estimated spend + cache savings (spend)
  mix               input / output / cache token composition (split)

OUTPUT MODES   (bypass the card)
  json              machine-readable summary (--stats); NDJSON for "all"
  quiet             one terse line for a shell prompt (-q)
  compare           all harnesses side by side (vs)

FLAGS
  -i, --tui         interactive mode (←/→ harness · tab · 1/2/3 range · q quit)
  -h, --help        this help

ENV
  NX_BACKGROUND     light|dark — override terminal background detection
  NX_TRUECOLOR      set to force 24-bit colour
  NX_TOKEN_NO_CACHE set to bypass the on-disk aggregate cache
  CLAUDE_CONFIG_DIR / CODEX_HOME / PI_AGENT_DIR — harness data roots

EXIT
  0 ok · 2 bad args · 3 no usage for the selection (output modes)

EXAMPLES
  nx token               nx token codex 7d cost      nx token claude punchcard
  nx token all json      nx token 30d compare        nx token pi trend -i

Nest help for one topic:
  nx help token harness · range · view · output · flags · env · exit · examples`
}
