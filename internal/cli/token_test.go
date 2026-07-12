package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/o1x3/nx/internal/token/core"
	"github.com/o1x3/nx/internal/token/ui"
)

func TestParseTokenArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want tokenOptions
	}{
		{
			name: "defaults",
			args: nil,
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "order independence a",
			args: []string{"7d", "cost", "claude"},
			want: tokenOptions{harness: core.Claude, rng: core.Range7d, tab: ui.TabCost},
		},
		{
			name: "order independence b",
			args: []string{"claude", "7d", "cost"},
			want: tokenOptions{harness: core.Claude, rng: core.Range7d, tab: ui.TabCost},
		},
		{
			name: "claude aliases",
			args: []string{"cc"},
			want: tokenOptions{harness: core.Claude, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "claude-code alias",
			args: []string{"claude-code"},
			want: tokenOptions{harness: core.Claude, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "codex alias",
			args: []string{"cx"},
			want: tokenOptions{harness: core.Codex, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "pi aliases",
			args: []string{"pi.dev"},
			want: tokenOptions{harness: core.Pi, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "pidev alias",
			args: []string{"pidev"},
			want: tokenOptions{harness: core.Pi, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "cursor",
			args: []string{"cursor"},
			want: tokenOptions{harness: core.Cursor, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "cursor-ide alias",
			args: []string{"cursor-ide"},
			want: tokenOptions{harness: core.Cursor, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "cursor-cli alias",
			args: []string{"cursor-cli"},
			want: tokenOptions{harness: core.Cursor, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "cursor-agent alias",
			args: []string{"cursor-agent"},
			want: tokenOptions{harness: core.Cursor, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "bare all is a harness not a range",
			args: []string{"all"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "combined alias",
			args: []string{"everything"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "range 30d aliases",
			args: []string{"month"},
			want: tokenOptions{harness: core.Combined, rng: core.Range30d, tab: ui.TabOverview},
		},
		{
			name: "range 30 numeric",
			args: []string{"30"},
			want: tokenOptions{harness: core.Combined, rng: core.Range30d, tab: ui.TabOverview},
		},
		{
			name: "range week alias",
			args: []string{"week"},
			want: tokenOptions{harness: core.Combined, rng: core.Range7d, tab: ui.TabOverview},
		},
		{
			name: "range alltime keyword",
			args: []string{"alltime"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "range lifetime keyword",
			args: []string{"lifetime"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview},
		},
		{
			name: "models tab aliases",
			args: []string{"-m"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabModels},
		},
		{
			name: "hours tab alias",
			args: []string{"clock"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabHours},
		},
		{
			name: "punchcard when alias",
			args: []string{"when"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabPunchcard},
		},
		{
			name: "trend spark alias",
			args: []string{"spark"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabTrend},
		},
		{
			name: "topdays busiest alias",
			args: []string{"busiest"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabTopDays},
		},
		{
			name: "weekday dow alias",
			args: []string{"dow"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabWeekday},
		},
		{
			name: "cost spend alias",
			args: []string{"spend"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabCost},
		},
		{
			name: "mix split alias",
			args: []string{"split"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabMix},
		},
		{
			name: "json mode",
			args: []string{"--stats"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview, jsonOut: true},
		},
		{
			name: "quiet mode",
			args: []string{"-q"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview, quiet: true},
		},
		{
			name: "compare vs alias",
			args: []string{"vs"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview, compare: true},
		},
		{
			name: "interactive tui alias",
			args: []string{"tui"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview, interactive: true},
		},
		{
			name: "help flag",
			args: []string{"--help"},
			want: tokenOptions{harness: core.Combined, rng: core.RangeAll, tab: ui.TabOverview, help: true},
		},
		{
			name: "everything at once",
			args: []string{"trend", "cursor", "-i", "30d"},
			want: tokenOptions{harness: core.Cursor, rng: core.Range30d, tab: ui.TabTrend, interactive: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTokenArgs(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("parseTokenArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseTokenArgsUnknown(t *testing.T) {
	for _, arg := range []string{"bogus", "--nope", "8d"} {
		t.Run(arg, func(t *testing.T) {
			_, err := parseTokenArgs([]string{arg})
			if err == nil {
				t.Fatal("expected error")
			}
			var exitErr ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("error %v is not an ExitError", err)
			}
			if exitErr.Code != 2 {
				t.Fatalf("exit code = %d, want 2", exitErr.Code)
			}
		})
	}
}

func TestRunTokenCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	err := New(BuildInfo{Version: "test"}).Run(context.Background(), []string{"token"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected card output on stdout")
	}
}

func TestRunTokensAliasDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	err := New(BuildInfo{Version: "test"}).Run(context.Background(), []string{"tokens", "--help"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected help output on stdout")
	}
}

func TestRunTokenQuietNoUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	err := New(BuildInfo{Version: "test"}).Run(context.Background(), []string{"token", "quiet"}, &stdout, &stderr)
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error %v is not an ExitError", err)
	}
	if exitErr.Code != 3 {
		t.Fatalf("exit code = %d, want 3", exitErr.Code)
	}
	if exitErr.Error() != "" {
		t.Fatalf("expected silent ExitError, got %q", exitErr.Error())
	}
	if got := stdout.String(); got != "nx token: no usage for this selection\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunTokenUnknownArgViaRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := New(BuildInfo{Version: "test"}).Run(context.Background(), []string{"token", "wat"}, &stdout, &stderr)
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error %v is not an ExitError", err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.Code)
	}
}
