#!/usr/bin/env sh
set -eu

fail=0

say() {
  printf '%s\n' "$*"
}

run() {
  say "==> $*"
  if "$@"; then
    return 0
  fi
  fail=1
  return 0
}

check_gofmt() {
  files="$(gofmt -l .)"
  if [ -n "$files" ]; then
    say "gofmt needed:"
    say "$files"
    fail=1
  fi
}

check_gofmt
run go test ./...
run sh -n scripts/format.sh
run sh -n scripts/install.sh
run sh -n scripts/check.sh
run sh -n scripts/validate-version.sh
run scripts/validate-version.sh
run sh -n .githooks/pre-commit

exit "$fail"
