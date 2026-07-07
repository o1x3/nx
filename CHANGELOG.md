# Changelog

## 0.0.3

- Improved `nx git stat` performance by fetching only the detected default branch instead of all `origin` refs.
- Improved multi-folder `nx git stat` collection with I/O-oriented concurrency and an `NX_GIT_STAT_JOBS` override while preserving output order.

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
