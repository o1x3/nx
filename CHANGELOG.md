# Changelog

## 0.1.3

- Fixed self-update noise on root-owned installs (e.g. `/usr/local/bin`): when the install directory is not writable, the daily update check now skips quietly instead of printing `permission denied`.

## 0.1.2

- Improved `nx token` load performance: harnesses and session files are parsed concurrently, Cursor SQLite databases open read-only in place (no temp copy unless the live file is locked), and parsed aggregates are cached under `~/.cache/nx/token` keyed by source file mtimes. Set `NX_TOKEN_NO_CACHE=1` to bypass the cache.
- Improved JSONL and Cursor blob parsing throughput with `github.com/bytedance/sonic` on the hot decode paths.

## 0.1.1

- Migrated `nx token` rendering to the charm.land v2 stack (`lipgloss/v2`, `bubbletea/v2`), removing the duplicate lipgloss v1/termenv/isatty dependency family; the whole binary now uses one styling stack. Output is unchanged.
- Improved color handling when piped or on limited terminals: ANSI is now stripped/downsampled at write time via `colorprofile`, and `NO_COLOR`/`CLICOLOR` are respected on auto-detected terminals.
- Improved truecolor fidelity: theme colors now emit their exact declared hex values (lipgloss v1 rounded some RGB channels off by one).
- Documented the `modernc.org/sqlite` dependency decision in AGENTS.md and cleaned up ported code to modern Go idioms (range-over-int, min/max, strings.Builder).

## 0.1.0

- Added the `nx token` command family (alias: `nx tokens`): a pastel terminal dashboard for AI coding-harness token usage, ported from tmax.
- Added four harnesses: Claude Code, Codex, pi.dev, and Cursor (IDE + `cursor-agent` CLI merged; Cursor tokens are estimated from transcript size (~4 bytes/token) since Cursor stores no real token counts locally).
- Added nine card views: overview, models, hours, punchcard, trend, topdays, weekday, cost, and mix, selectable with order-independent positional arguments alongside harness and range (`alltime`, `30d`, `7d`).
- Improved model display names over tmax: legacy Claude ids render as "Sonnet 3.5"-style names (e.g. `claude-3-5-sonnet-20241022` → "Sonnet 3.5", where tmax rendered "3 5.sonnet").
- Added standalone output modes: `json` (NDJSON for `all`), `quiet` one-liner for shell prompts, and `compare` side-by-side harness view.
- Added an interactive TUI (`nx token -i`) with harness/tab/range navigation.
- Added adaptive light/dark rendering with `NX_BACKGROUND` and `NX_TRUECOLOR` overrides, plus plain-text output when piped.
- Added distinct exit codes for scripting: 0 ok, 2 bad arguments, 3 no usage for the selection (output modes).

## 0.0.4

- Fixed `nx git stat` default-branch fallback when `git remote show -n origin` reports `HEAD branch: (not queried)`.

## 0.0.3

- Improved `nx git stat` performance by fetching only the detected default branch instead of all `origin` refs.
- Improved multi-folder `nx git stat` collection with concurrent folder checks while preserving input order.
- Added `nx git stat --jobs <n>` to tune multi-folder collection concurrency.

## 0.0.2

- Added the initial extensible `nx` Go CLI foundation.
- Added `nx git stat <folder> [folder...]` with pretty terminal output.
- Added verified curl installer and runtime self-update from GitHub releases.
- Added VERSION-driven release automation with GoReleaser.
- Added local format/check scripts and pre-commit hook support.
- Added architecture notes for future command additions.

## 0.0.1

- Initial `nx` CLI foundation.
- Added `nx git stat <folder> [folder...]`.
- Added daily self-update checks from GitHub releases.
