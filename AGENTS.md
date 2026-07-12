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
- `internal/selfupdate`: daily GitHub release checks and binary replacement.
- `internal/token`: coding-agent token/cost usage stats across harnesses (claude, codex, pi, cursor), with `core` collection, `ui` rendering, and `tui` interactive views.

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

## Adding Commands

Each new command should add or extend one domain package under `internal/<domain>`, then expose only the routing surface through `internal/cli`.

The expected change shape is:

- domain behavior in `internal/<domain>`
- CLI routing in `internal/cli`
- rendering isolated from collection when terminal output is non-trivial
- behavior tests for the domain package
- README command docs when user-facing behavior changes

## Release Model

`VERSION` is the release trigger. GoReleaser owns GitHub releases and macOS/Linux artifacts. Runtime self-update consumes the latest GitHub release asset for the current OS and architecture.

When a user asks for a command to be built and deployed, the expected final change includes:

- the command implementation
- tests and README updates
- a patch/minor/major bump in `VERSION`
- a matching `CHANGELOG.md` section for that version

Pushing a `VERSION` change to `main` creates tag `v<VERSION>` and publishes the release. Existing installations pick it up through self-update.
