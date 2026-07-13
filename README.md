# nx

`nx` is a personal development CLI. It starts small: a command framework that can grow, plus a pretty git branch stats command.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/o1x3/nx/main/scripts/install.sh | sh
```

By default this installs to `/usr/local/bin/nx`, which is usually on `PATH` on macOS and common Linux setups.

Override the install directory when needed:

```sh
curl -fsSL https://raw.githubusercontent.com/o1x3/nx/main/scripts/install.sh | NX_INSTALL_DIR="$HOME/.local/bin" sh
```

## Commands

```sh
nx git stat [--jobs <n>] <folder> [folder...]
```

Example:

```sh
nx git stat gigauser gigauser-backend-prod the-exchange
```

What it does:

- Treats each folder as a path relative to your current directory.
- Fetches only the detected `origin` default branch.
- Auto-detects the remote default branch from `origin/HEAD`.
- Falls back to `origin/main` if default branch detection is unavailable.
- Checks multiple folders concurrently; set `--jobs <n>` to tune concurrency.
- Prints changed files, added lines, and removed lines for `<base>...HEAD`.

```sh
nx token [harness] [range] [view] [-i]
nx token [harness] [range] (json | quiet | compare)
```

A pastel terminal dashboard for your AI coding-harness token usage (alias: `nx tokens`). All arguments are positional and order-independent.

- Harness (default `all`): `claude` (`cc`, `claude-code`), `codex` (`cx`), `pi` (`pi.dev`, `pidev`), `cursor` (`cursor-ide`, `cursor-cli`, `cursor-agent`), `all` (`combined`, `everything`).
- Range (default all-time): `alltime` (`lifetime`), `30d` (`month`, `30`), `7d` (`week`, `7`).
- View (default `overview`): `overview`, `models`, `hours`, `punchcard`, `trend`, `topdays`, `weekday`, `cost`, `mix`.
- Output modes (bypass the card): `json` (NDJSON for `all`), `quiet` (`-q`, one prompt-safe line), `compare` (`vs`, side by side).
- Flags: `-i`/`--tui` interactive mode, `-h`/`--help`.

Examples:

```sh
nx token                    # combined card, all harnesses, all time
nx token codex 7d cost      # Codex spend, last 7 days
nx token claude punchcard   # Claude Code weekday × hour grid
nx token all json | jq -s   # NDJSON summary, one object per harness
nx token -i                 # interactive: ←/→ harness · tab/⇧tab views · 1/2/3 range · q quit
```

Data sources:

| Harness | Source | Tokens |
| --- | --- | --- |
| Claude Code | `~/.claude/projects/*/*.jsonl` | real |
| Codex | `~/.codex/sessions/**/rollout-*.jsonl` | real |
| pi.dev | `~/.pi/agent/sessions/*/*.jsonl` | real |
| Cursor (IDE + CLI) | `<config>/Cursor/User/globalStorage/state.vscdb` + `~/.cursor/chats/*/*/store.db` | estimated from transcript size (~4 bytes/token) |

Cursor does not store real token counts locally, so its figures are estimates. `<config>` is `~/Library/Application Support` on macOS and `~/.config` on Linux.

Environment:

- `NX_BACKGROUND=light|dark` overrides terminal background detection.
- `NX_TRUECOLOR=1` forces 24-bit colour (useful for capture tools). Piped output is plain text.
- `NX_TOKEN_NO_CACHE=1` bypasses the on-disk aggregate cache for `nx token` (useful when debugging or forcing a full re-scan).

Exit codes: `0` ok · `2` bad arguments · `3` no usage for the selection (output modes only, so it composes in scripts and CI).

Interactive keys: `←`/`→` switch harness, `tab`/`⇧tab` cycle views, `1`/`2`/`3` set the range, `q` quits.

## Updates

Released builds check GitHub for the latest release at most once per day. If a newer release exists for your OS and CPU, `nx` downloads it and replaces the current binary in place.

Disable update checks:

```sh
NX_NO_UPDATE=1 nx git stat .
```

Development builds with version `dev` do not auto-update.

## Development

```sh
go test ./...
go run ./cmd/nx git stat .
```

Install local git hooks:

```sh
scripts/install-hooks.sh
```

The pre-commit hook runs `scripts/format.sh` first, then `scripts/check.sh`. Add future formatters to `scripts/format.sh` and future validators to `scripts/check.sh`.

## Release Automation

Releases are driven by `VERSION` and published by GoReleaser.

To release, update `VERSION` and add a matching section to `CHANGELOG.md`, then push to `main`. GitHub Actions creates tag `v<VERSION>` and publishes macOS/Linux `amd64` and `arm64` archives.

The installer and auto-updater consume the latest GitHub release assets.

This project is seeded at `0.0.1`.

This project is seeded at `0.0.1`. The initial push to `main` with `VERSION=0.0.1` publishes `v0.0.1`.

For later releases:

```sh
printf '0.0.2\n' > VERSION
# add ## 0.0.2 to CHANGELOG.md
git commit -am "release: v0.0.2"
git push origin main
```

OpenRouter-backed release note enrichment is intentionally not wired yet. Add it only after the secret name, prompt, and failure behavior are explicit.

## Design Notes

The command framework is intentionally small. `nx git stat` and `nx token` are routed through an internal command dispatcher, and each concern is separated:

- `internal/cli`: command routing
- `internal/gitstat`: git collection logic
- `internal/render`: terminal rendering
- `internal/token`: token-usage dashboard (`core` collection/estimation, `ui` static views, `tui` interactive mode)
- `internal/selfupdate`: daily release update checks

No Cobra dependency yet. That is reversible if the command tree becomes large enough to justify it.
