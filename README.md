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
nx git stat <folder> [folder...]
```

Example:

```sh
nx git stat gigauser gigauser-backend-prod the-exchange
```

What it does:

- Treats each folder as a path relative to your current directory.
- Fetches the detected `origin` default branch.
- Auto-detects the remote default branch from `origin/HEAD`.
- Falls back to `origin/main` if default branch detection is unavailable.
- Checks multiple folders concurrently; set `NX_GIT_STAT_JOBS=<n>` to tune concurrency.
- Prints changed files, added lines, and removed lines for `<base>...HEAD`.

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

The command framework is intentionally small. `nx git stat` is routed through an internal command dispatcher, and each concern is separated:

- `internal/cli`: command routing
- `internal/gitstat`: git collection logic
- `internal/render`: terminal rendering
- `internal/selfupdate`: daily release update checks

No Cobra dependency yet. That is reversible if the command tree becomes large enough to justify it.
