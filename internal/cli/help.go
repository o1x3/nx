package cli

import (
	"fmt"
	"io"
	"strings"
)

// runHelp prints nested help for a command path.
// Examples: nx help · nx help git · nx help git stat · nx help token harness
func (a App) runHelp(args []string, stdout io.Writer) error {
	text, err := helpFor(args)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, text)
	return nil
}

func helpFor(args []string) (string, error) {
	if len(args) == 0 {
		return helpText(), nil
	}

	switch args[0] {
	case "git":
		return gitHelpFor(args[1:])
	case "token", "tokens":
		return tokenHelpFor(args[1:])
	case "update":
		if len(args) > 1 {
			return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[1], strings.TrimSpace(updateHelpText()))}
		}
		return updateHelpText(), nil
	case "version", "-v", "--version":
		if len(args) > 1 {
			return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[1], strings.TrimSpace(versionHelpText()))}
		}
		return versionHelpText(), nil
	case "help", "-h", "--help":
		if len(args) > 1 {
			return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[1], strings.TrimSpace(helpCommandHelpText()))}
		}
		return helpCommandHelpText(), nil
	default:
		return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[0], strings.TrimSpace(helpText()))}
	}
}

func gitHelpFor(args []string) (string, error) {
	if len(args) == 0 {
		return gitHelpText(), nil
	}
	switch args[0] {
	case "stat", "stats":
		if len(args) > 1 {
			return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[1], strings.TrimSpace(gitStatHelpText()))}
		}
		return gitStatHelpText(), nil
	case "help", "-h", "--help":
		return gitHelpText(), nil
	default:
		return "", ExitError{Code: 2, Err: fmt.Errorf("unknown git help topic %q\n\n%s", args[0], strings.TrimSpace(gitHelpText()))}
	}
}

func tokenHelpFor(args []string) (string, error) {
	if len(args) == 0 {
		return tokenHelpText() + "\n", nil
	}
	topic := args[0]
	text := tokenTopicHelpText(topic)
	if text == "" {
		return "", ExitError{Code: 2, Err: fmt.Errorf("unknown token help topic %q\n\n%s", topic, strings.TrimSpace(tokenHelpTopicsText()))}
	}
	if len(args) > 1 {
		return "", ExitError{Code: 2, Err: fmt.Errorf("unknown help topic %q\n\n%s", args[1], strings.TrimSpace(text))}
	}
	return text, nil
}

func helpText() string {
	return `nx is a personal development CLI.

Usage:
  nx <command> [args]
  nx help [command...]

Commands:
  git       Git repository helpers
  token     Token stats across AI coding harnesses
  update    Update nx to the latest release
  version   Show build version
  help      Show help (nest for details)

Nest help to learn more:
  nx help git
  nx help token
  nx help update
  nx help version

`
}

func updateHelpText() string {
	return `Usage:
  nx update

Check GitHub releases for a newer nx build and replace this binary in place
when one is available. Released builds also check on every other nx invocation.

Notes:
  - Resolves the latest tag via github.com/.../releases/latest (not the API)
  - Needs a writable install directory (default ~/.local/bin)
  - Development builds (version "dev") cannot self-update
  - Set NX_NO_UPDATE=1 to disable background checks (nx update still works)

Examples:
  nx update
  NX_NO_UPDATE=1 nx version

`
}

func helpCommandHelpText() string {
	return `Usage:
  nx help [command...]

Show help for a command path. Keep nesting to see deeper detail.

Examples:
  nx help
  nx help git
  nx help git stat
  nx help token
  nx help token harness

`
}

func versionHelpText() string {
	return `Usage:
  nx version

Show the build version, commit, and build date.

Aliases:
  -v, --version

`
}

func gitHelpText() string {
	return `Usage:
  nx git <subcommand> [args]
  nx help git [subcommand]

Subcommands:
  stat   Show branch diff stats against the repo default branch

Nest help:
  nx help git stat

`
}

func gitStatHelpText() string {
	return `Usage:
  nx git stat [--jobs <n>] <folder> [folder...]

Show changed files, added lines, and removed lines for each folder's
branch against the remote default branch (<base>...HEAD).

Arguments:
  <folder>   Path relative to the current working directory

Options:
  --jobs <n>   Max concurrent folder checks (default: GOMAXPROCS)

Notes:
  - Fetches only the detected origin default branch
  - Auto-detects the remote default branch from origin/HEAD
  - Falls back to origin/main if default branch detection is unavailable

Examples:
  nx git stat .
  nx git stat --jobs 4 repo-a repo-b

`
}

func tokenHelpTopicsText() string {
	return `Usage:
  nx help token [topic]

Topics:
  harness   Claude / Codex / pi / Cursor sources
  range     alltime / 30d / 7d
  view      overview, models, hours, punchcard, ...
  output    json / quiet / compare modes
  flags     -i / --help
  env       NX_* / CLAUDE_CONFIG_DIR / CODEX_HOME / PI_AGENT_DIR
  exit      process exit codes
  examples  common invocations

Or show everything:
  nx help token
  nx token --help

`
}

func tokenTopicHelpText(topic string) string {
	switch topic {
	case "help", "-h", "--help", "topics":
		return tokenHelpTopicsText()
	case "harness", "harnesses", "source", "sources":
		return `nx token — harnesses

HARNESS   (default: all)
  claude            Claude Code        ~/.claude + ~/.config/claude
  codex             OpenAI Codex       ~/.codex (sessions + archived)
  pi                pi.dev             ~/.pi/agent/sessions
  cursor            Cursor IDE + CLI   state.vscdb + ~/.cursor
  all               every harness merged

Aliases:
  claude: cc, claude-code
  codex:  cx
  pi:     pi.dev, pidev
  cursor: cursor-ide, cursor-cli, cursor-agent
  all:    combined, everything

Overrides: CLAUDE_CONFIG_DIR, CODEX_HOME, PI_AGENT_DIR (comma-separated paths).

Claude counts final streaming chunks (stop_reason) and includes subagent
JSONL. Codex prefers last_token_usage deltas and skips archived duplicates.
Cursor prefers real bubble tokenCount when present; otherwise credits the
composer context meter once per chat, else ~4 bytes/token. Cursor Auto
resolves to the underlying local model (AgentKv / usageData) when available.
Local Cursor totals undercount the admin dashboard (cache/billed cumulatives
are server-side).

More: nx help token

`
	case "range", "ranges", "time":
		return `nx token — ranges

RANGE     (default: alltime)
  alltime           lifetime
  30d               last 30 days
  7d                last 7 days

Aliases:
  alltime: lifetime
  30d:     month, 30
  7d:      week, 7

More: nx help token

`
	case "view", "views", "tab", "tabs":
		return `nx token — views

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

More: nx help token

`
	case "output", "outputs", "mode", "modes":
		return `nx token — output modes

OUTPUT MODES   (bypass the card)
  json              machine-readable summary (--stats); NDJSON for "all"
  quiet             one terse line for a shell prompt (-q)
  compare           all harnesses side by side (vs)

These modes exit 3 when the selection has no usage, so they compose in scripts.

More: nx help token

`
	case "flag", "flags", "options":
		return `nx token — flags

FLAGS
  -i, --tui         interactive mode (←/→ harness · tab · 1/2/3 range · q quit)
  -h, --help        this help

Interactive aliases: --interactive, -t, tui

More: nx help token

`
	case "env", "environment":
		return `nx token — environment

ENV
  NX_BACKGROUND     light|dark — override terminal background detection
  NX_TRUECOLOR      set to force 24-bit colour
  NX_TOKEN_NO_CACHE set to bypass the on-disk aggregate cache
  CLAUDE_CONFIG_DIR comma-separated Claude config roots (…/projects)
  CODEX_HOME        comma-separated Codex homes
  PI_AGENT_DIR      comma-separated pi-agent session dirs

Piped output is plain text. NO_COLOR / CLICOLOR are respected.

More: nx help token

`
	case "exit", "exits", "codes", "status":
		return `nx token — exit codes

EXIT
  0 ok
  2 bad args
  3 no usage for the selection (output modes only)

More: nx help token

`
	case "example", "examples":
		return `nx token — examples

EXAMPLES
  nx token               nx token codex 7d cost      nx token claude punchcard
  nx token all json      nx token 30d compare        nx token pi trend -i

More: nx help token

`
	default:
		return ""
	}
}
