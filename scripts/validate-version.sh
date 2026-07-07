#!/usr/bin/env sh
set -eu

version="$(cat VERSION)"

case "$version" in
  *[!0-9.]* | "" | .* | *. | *..*)
    echo "VERSION must be plain semver like 0.0.1" >&2
    exit 1
    ;;
esac

old_ifs="$IFS"
IFS=.
set -- $version
IFS="$old_ifs"

if [ "$#" -ne 3 ]; then
  echo "VERSION must have major.minor.patch" >&2
  exit 1
fi

for part in "$@"; do
  case "$part" in
    "" | *[!0-9]*)
      echo "VERSION must have numeric major.minor.patch parts" >&2
      exit 1
      ;;
  esac
done

if ! grep -Eq "^##[[:space:]]+v?${version}([[:space:]]|$)" CHANGELOG.md; then
  echo "CHANGELOG.md must include a section for ${version}" >&2
  exit 1
fi
