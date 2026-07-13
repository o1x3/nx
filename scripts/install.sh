#!/usr/bin/env sh
set -eu

repo="${NX_REPO:-o1x3/nx}"
# Prefer a user-writable bindir so runtime self-update can replace the binary.
# /usr/local/bin is often root-owned and breaks in-place updates.
if [ -n "${NX_INSTALL_DIR:-}" ]; then
  install_dir="$NX_INSTALL_DIR"
else
  install_dir="${HOME}/.local/bin"
fi
binary="${install_dir}/nx"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

api="https://api.github.com/repos/${repo}/releases/latest"
release_json="$tmp/release.json"
curl -fsSL "$api" -o "$release_json"

asset_url="$(
  awk -v os="$os" -v arch="$arch" '
    /browser_download_url/ {
      url=$2
      gsub(/[",]/, "", url)
      low=tolower(url)
      if (index(low, os) && index(low, arch) && low ~ /\.tar\.gz$/) {
        print url
        exit
      }
    }
  ' "$release_json"
)"

checksum_url="$(
  awk '
    /browser_download_url/ {
      url=$2
      gsub(/[",]/, "", url)
      low=tolower(url)
      if (low ~ /checksums\.txt$/) {
        print url
        exit
      }
    }
  ' "$release_json"
)"

if [ -z "$asset_url" ]; then
  echo "could not find nx release asset for ${os}/${arch}" >&2
  exit 1
fi

if [ -z "$checksum_url" ]; then
  echo "could not find checksums.txt release asset" >&2
  exit 1
fi

archive_name="$(basename "$asset_url")"
curl -fsSL "$asset_url" -o "$tmp/$archive_name"
curl -fsSL "$checksum_url" -o "$tmp/checksums.txt"

expected="$(
  awk -v name="$archive_name" '
    {
      file=$2
      sub(/^\*/, "", file)
      if (file == name) {
        print $1
        exit
      }
    }
  ' "$tmp/checksums.txt"
)"

if [ -z "$expected" ]; then
  echo "checksums.txt has no entry for $archive_name" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive_name" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$archive_name" | awk '{print $1}')"
else
  echo "need sha256sum or shasum to verify release archive" >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  echo "checksum mismatch for $archive_name" >&2
  exit 1
fi

tar -xzf "$tmp/$archive_name" -C "$tmp" nx
chmod 0755 "$tmp/nx"

if ! mkdir -p "$install_dir" 2>/dev/null; then
  echo "cannot create install directory: $install_dir" >&2
  echo "set NX_INSTALL_DIR to a writable path (default: \$HOME/.local/bin)" >&2
  exit 1
fi

# Refuse root-owned / non-writable bindirs: self-update needs to write beside the binary.
probe="$install_dir/.nx-install-write-$$"
if ! ( : >"$probe" ) 2>/dev/null; then
  echo "install directory is not writable: $install_dir" >&2
  echo "nx self-update requires a user-writable bindir; try:" >&2
  echo "  curl -fsSL https://raw.githubusercontent.com/${repo}/main/scripts/install.sh | NX_INSTALL_DIR=\"\$HOME/.local/bin\" sh" >&2
  exit 1
fi
rm -f "$probe"

if ! install -m 0755 "$tmp/nx" "$binary" 2>/dev/null; then
  # BusyBox / minimal environments may lack install(1).
  if ! cp "$tmp/nx" "$binary"; then
    echo "failed to install nx to $binary" >&2
    exit 1
  fi
  chmod 0755 "$binary"
fi

echo "installed nx to $binary"

case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *)
    echo "note: ${install_dir} is not on PATH; add it so \`nx\` resolves after install" >&2
    ;;
esac
