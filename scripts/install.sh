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
path_marker="# managed by nx installer"

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

# Capture any pre-existing nx before we rewrite PATH for this process.
previous_nx=""
if command -v nx >/dev/null 2>&1; then
  previous_nx="$(command -v nx)"
fi

ensure_path_in_profile() {
  profile="$1"
  bindir="$2"

  if [ -f "$profile" ] && grep -Fq "$path_marker" "$profile" 2>/dev/null; then
    return 0
  fi

  if [ ! -e "$profile" ]; then
    # Only create common profile files; do not invent obscure shell configs.
    case "$profile" in
      */.profile|*/.zprofile|*/.zshrc|*/.bashrc|*/.bash_profile) ;;
      *) return 0 ;;
    esac
  fi

  {
    printf '\n%s\n' "$path_marker"
    printf 'case ":$PATH:" in\n'
    printf '  *":%s:"*) ;;\n' "$bindir"
    printf '  *) export PATH="%s:$PATH" ;;\n' "$bindir"
    printf 'esac\n'
  } >>"$profile"
  echo "added ${bindir} to PATH in ${profile}"
}

ensure_install_dir_on_path() {
  bindir="$1"

  # Current process / this shell session.
  case ":$PATH:" in
    *":$bindir:"*) ;;
    *) export PATH="${bindir}:${PATH}" ;;
  esac

  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh)
      ensure_path_in_profile "${HOME}/.zprofile" "$bindir"
      ensure_path_in_profile "${HOME}/.zshrc" "$bindir"
      ;;
    bash)
      ensure_path_in_profile "${HOME}/.bashrc" "$bindir"
      ensure_path_in_profile "${HOME}/.bash_profile" "$bindir"
      ensure_path_in_profile "${HOME}/.profile" "$bindir"
      ;;
    fish)
      fish_config="${HOME}/.config/fish/config.fish"
      mkdir -p "$(dirname "$fish_config")"
      if [ -f "$fish_config" ] && grep -Fq "$path_marker" "$fish_config" 2>/dev/null; then
        :
      else
        {
          printf '\n%s\n' "$path_marker"
          printf 'fish_add_path %s\n' "$bindir"
        } >>"$fish_config"
        echo "added ${bindir} to PATH in ${fish_config}"
      fi
      ;;
    *)
      ensure_path_in_profile "${HOME}/.profile" "$bindir"
      ;;
  esac
}

remove_stale_nx() {
  candidate="$1"
  case "$candidate" in
    /*) ;;
    *) return 0 ;;
  esac
  if [ ! -e "$candidate" ] && [ ! -L "$candidate" ]; then
    return 0
  fi
  # Resolve to a canonical path when possible so we do not delete the new install.
  resolved="$candidate"
  if command -v realpath >/dev/null 2>&1; then
    resolved="$(realpath "$candidate" 2>/dev/null || printf '%s' "$candidate")"
  elif command -v readlink >/dev/null 2>&1; then
    resolved="$(readlink -f "$candidate" 2>/dev/null || printf '%s' "$candidate")"
  fi
  target_resolved="$binary"
  if command -v realpath >/dev/null 2>&1; then
    target_resolved="$(realpath "$binary" 2>/dev/null || printf '%s' "$binary")"
  elif command -v readlink >/dev/null 2>&1; then
    target_resolved="$(readlink -f "$binary" 2>/dev/null || printf '%s' "$binary")"
  fi
  if [ "$resolved" = "$target_resolved" ] || [ "$candidate" = "$binary" ]; then
    return 0
  fi

  if rm -f "$candidate" 2>/dev/null; then
    echo "removed previous install at $candidate"
    return 0
  fi
  if command -v sudo >/dev/null 2>&1 && sudo rm -f "$candidate"; then
    echo "removed previous install at $candidate"
    return 0
  fi
  echo "warning: could not remove previous install at $candidate; it may shadow ${binary} on PATH" >&2
}

migrate_previous_installs() {
  # Former default location from earlier installers.
  remove_stale_nx "/usr/local/bin/nx"

  # Binary that resolved as `nx` before this install rewrote PATH.
  if [ -n "$previous_nx" ]; then
    remove_stale_nx "$previous_nx"
  fi

  # Refresh the shell's command hash table so `command -v` reflects removals.
  hash -r 2>/dev/null || true
}

ensure_install_dir_on_path "$install_dir"
migrate_previous_installs

hash -r 2>/dev/null || true
resolved="$(command -v nx 2>/dev/null || true)"
if [ -n "$resolved" ]; then
  if [ "$resolved" = "$binary" ]; then
    echo "nx is on PATH as $resolved"
  else
    echo "warning: \`nx\` currently resolves to $resolved, not $binary" >&2
    echo "open a new shell, or put ${install_dir} earlier on PATH" >&2
  fi
else
  echo "note: open a new shell so ${install_dir} is on PATH, then run: nx version" >&2
fi
