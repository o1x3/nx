# nx Architecture Notes

`nx` is a personal developer CLI, not a framework product. Keep the core small until repeated command patterns justify more structure.

## Shape

- `cmd/nx` owns process startup, build metadata, and top-level wiring.
- `internal/cli` owns command routing and should stay thin.
- Domain packages under `internal/<domain>` own behavior.
- Rendering stays separate from collection so commands remain testable without terminal snapshots.

Current domains:

- `internal/gitstat`: repository diff stats against the remote default branch.
- `internal/render`: Lip Gloss terminal presentation.
- `internal/selfupdate`: per-invocation GitHub release checks (via `releases/latest`, not the API) and binary replacement; also backs `nx update`.
- `internal/token`: coding-agent token/cost usage stats across harnesses (claude, codex, pi, cursor), with `core` collection, `ui` rendering, and `tui` interactive views.

Dependency decision: `internal/token` reads Cursor's SQLite stores through `modernc.org/sqlite` (pure Go, ~4 MB added to the stripped binary). Rejected alternatives: `mattn/go-sqlite3` needs cgo and breaks the `CGO_ENABLED=0` cross-compiled releases; shelling out to a system `sqlite3` is a fragile runtime dependency; a hand-written SQLite/WAL reader is a correctness risk.

Commands signal specific exit codes by returning `cli.ExitError` (0 ok, 2 usage error, 3 no data, 1 anything else).

## Command Model

Commands should grow as explicit namespaces:

```sh
nx <domain> <verb> [args]
```

Example:

```sh
nx git stat repo-a repo-b
```

Do not add root-level shortcuts unless they are clearly permanent. Folder arguments are current-working-directory relative; discovery outside the provided paths belongs in a separate command.

## Nested Help

Help is a nestable discovery tree, not a single dump. Users learn the CLI by drilling in:

```sh
nx help                 # root: list top-level commands + nest hints
nx help <domain>        # domain overview + subcommands/topics
nx help <domain> <verb> # verb / topic detail
```

Examples that must keep working: `nx help git`, `nx help git stat`, `nx help token`, `nx help token harness`.

Precedent (keep this current when commands change):

- Help routing and text live in `internal/cli/help.go` (`helpFor` / `runHelp`). Root `help` / `-h` / `--help` forwards leftover args as the help path.
- Every user-facing command path gets a matching `nx help …` page. Domain overviews list next-level nest targets; leaf pages cover usage, args, flags, and examples.
- Large surfaces (like `token`) expose focused topic pages under `nx help <domain> <topic>` in addition to the full page. Prefer nesting over stuffing more into the root blurb.
- Domain-local help should reuse the same tree (`nx git help [stat]`, `nx token --help`), not a divergent second copy of the docs.
- Unknown help paths return `cli.ExitError` code `2` and print the nearest help page so users can recover by nesting up/down.
- When you add or rename a command, subcommand, flag, or token topic: update the nested help text, the root nest hints if needed, `internal/cli/help_test.go`, and the README Help section in the same change.

## Adding Commands

Each new command should add or extend one domain package under `internal/<domain>`, then expose only the routing surface through `internal/cli`.

The expected change shape is:

- domain behavior in `internal/<domain>`
- CLI routing in `internal/cli`
- nested help pages in `internal/cli/help.go` (and topics when the surface is large)
- rendering isolated from collection when terminal output is non-trivial
- behavior tests for the domain package (plus help-path coverage for new nest targets)
- README command docs when user-facing behavior changes

## Release Model

`VERSION` is the release trigger. GoReleaser owns GitHub releases and macOS/Linux artifacts. Runtime self-update consumes the latest GitHub release asset for the current OS and architecture.

When a user asks for a command to be built and deployed, the expected final change includes:

- the command implementation
- tests and README updates
- a patch/minor/major bump in `VERSION`
- a matching `CHANGELOG.md` section for that version

Pushing a `VERSION` change to `main` creates tag `v<VERSION>` and publishes the release. Existing installations pick it up through self-update.

## Cursor Cloud specific instructions

`nx` is a single Go CLI (no servers/databases). Standard commands live in `README.md` ("Development") and `scripts/check.sh`; use those. Notes below are only the non-obvious caveats.

- Toolchain: `go.mod` pins `go 1.25.0`; the `go` toolchain auto-downloads it on first use, so no manual Go install is needed.
- Run in dev with `go run ./cmd/nx <cmd>` (e.g. `go run ./cmd/nx git stat .`). Local/`go run` builds report version `dev` and never self-update, so `NX_NO_UPDATE=1` is unnecessary for dev.
- `nx token` reads local AI-harness data dirs (`~/.claude`, `~/.codex`, `~/.pi`, Cursor SQLite stores). A fresh VM has none, so the dashboard shows "No tokens recorded yet" and output modes (`json`/`quiet`/`compare`) exit `3`. That is expected, not a failure.
- Full local gate: `scripts/check.sh` (runs `gofmt -l .`, `go test ./...`, `sh -n` on scripts, and version validation). `scripts/validate-version.sh` requires `VERSION` to have a matching `CHANGELOG.md` section, so bumping `VERSION` without a changelog entry fails the gate.
- Commit authorship: the repo owner does NOT want to be added as a `Co-authored-by:` on agent commits. A Cursor-managed hook (`commit-msg.cursor.co-author` under the VM's agent hooks dir) auto-appends that trailer and may be regenerated on fresh VMs; disable it (e.g. `chmod -x` the hook) before committing, and do not add a `Co-authored-by:` trailer manually.
