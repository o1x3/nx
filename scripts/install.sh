#!/usr/bin/env sh
set -eu

repo="${NX_REPO:-o1x3/nx}"
install_dir="${NX_INSTALL_DIR:-/usr/local/bin}"
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

if [ ! -d "$install_dir" ]; then
  if mkdir -p "$install_dir" 2>/dev/null; then
    :
  else
    sudo mkdir -p "$install_dir"
  fi
fi

if install -m 0755 "$tmp/nx" "$binary" 2>/dev/null; then
  :
else
  sudo install -m 0755 "$tmp/nx" "$binary"
  sudo chown "$(id -u):$(id -g)" "$binary" || true
fi

echo "installed nx to $binary"
