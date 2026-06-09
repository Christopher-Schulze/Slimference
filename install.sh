#!/usr/bin/env bash
# Install the local Slimference TUI/CLI binary on macOS.
#
# This installer is intentionally scoped: it builds or copies the binary into
# ~/.local/bin and makes PATH setup visible. It does not arm Codex routing,
# install trusted CA material, or change global network settings.

set -euo pipefail

VERSION="0.6.0"
REPO_URL="https://github.com/Christopher-Schulze/Slimference"
INSTALL_DIR="${SLIMFERENCE_INSTALL_DIR:-$HOME/.local/bin}"
BIN_PATH="$INSTALL_DIR/slimference"
SOURCE_DIR=""

usage() {
  cat <<USAGE
Slimference ${VERSION} installer

Usage:
  ./install.sh [--from-binary PATH] [--release VERSION|latest] [--no-path-hint] [--verify-only]

Options:
  --from-binary PATH   Install an existing slimference binary instead of building
                       from this source checkout.
  --release VERSION    Download a GitHub release when no local source checkout
                       is available. Defaults to latest for raw GitHub installs.
  --no-path-hint       Do not print shell profile guidance.
  --verify-only        Only verify the installed binary and preflight status.
  -h, --help           Show this help.

Repository:
  ${REPO_URL}
USAGE
}

FROM_BINARY=""
RELEASE_VERSION="${SLIMFERENCE_RELEASE:-latest}"
PATH_HINT=1
VERIFY_ONLY=0
DOWNLOAD_DIR=""
BUILD_TMP=""

cleanup() {
  if [[ -n "$DOWNLOAD_DIR" ]]; then
    rm -rf "$DOWNLOAD_DIR"
  fi
  if [[ -n "$BUILD_TMP" ]]; then
    rm -f "$BUILD_TMP"
  fi
}
trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from-binary)
      [[ $# -ge 2 ]] || { echo "--from-binary requires a path" >&2; exit 2; }
      FROM_BINARY="$2"
      shift 2
      ;;
    --release)
      [[ $# -ge 2 ]] || { echo "--release requires a version or latest" >&2; exit 2; }
      RELEASE_VERSION="$2"
      shift 2
      ;;
    --no-path-hint)
      PATH_HINT=0
      shift
      ;;
    --verify-only)
      VERIFY_ONLY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

SCRIPT_ON_DISK=0
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  SCRIPT_ON_DISK=1
  SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
else
  SOURCE_DIR="$PWD"
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "Slimference v${VERSION} is currently supported on macOS only." >&2
  exit 1
fi

ensure_path_hint() {
  if [[ ":$PATH:" == *":$INSTALL_DIR:"* || "$PATH_HINT" -eq 0 ]]; then
    return
  fi
  local shell_name profile
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh) profile="$HOME/.zshrc" ;;
    bash) profile="$HOME/.bashrc" ;;
    *) profile="$HOME/.profile" ;;
  esac
  cat <<PATHMSG

PATH setup needed:
  export PATH="$INSTALL_DIR:\$PATH"

Add that line to:
  $profile

Then open a new terminal, or run:
  export PATH="$INSTALL_DIR:\$PATH"
PATHMSG
}

verify_install() {
  if [[ ! -x "$BIN_PATH" ]]; then
    echo "Slimference binary is not installed at $BIN_PATH" >&2
    exit 1
  fi
  "$BIN_PATH" --version
  "$BIN_PATH" status --preflight || true
}

release_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64) echo "amd64" ;;
    *)
      echo "Unsupported macOS architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

resolve_release_tag() {
  local requested="$1"
  if [[ "$requested" != "latest" ]]; then
    case "$requested" in
      v*) echo "$requested" ;;
      *) echo "v$requested" ;;
    esac
    return
  fi
  local effective tag
  effective="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$REPO_URL/releases/latest")"
  tag="${effective##*/}"
  if [[ -z "$tag" || "$tag" == "latest" ]]; then
    echo "Could not resolve latest Slimference release from $REPO_URL/releases/latest" >&2
    exit 1
  fi
  echo "$tag"
}

download_release_binary() {
  command -v curl >/dev/null 2>&1 || { echo "curl is required to install from GitHub releases." >&2; exit 1; }
  command -v tar >/dev/null 2>&1 || { echo "tar is required to install from GitHub releases." >&2; exit 1; }

  local tag ver arch archive url
  tag="$(resolve_release_tag "$RELEASE_VERSION")"
  ver="${tag#v}"
  arch="$(release_arch)"
  archive="slimference_${ver}_darwin_${arch}.tar.gz"
  url="$REPO_URL/releases/download/$tag/$archive"
  DOWNLOAD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/slimference-install.XXXXXX")"

  echo "Downloading Slimference $tag for macOS/$arch:"
  echo "  $url"
  curl -fL --retry 3 --connect-timeout 10 "$url" -o "$DOWNLOAD_DIR/$archive"
  tar -xzf "$DOWNLOAD_DIR/$archive" -C "$DOWNLOAD_DIR"
  FROM_BINARY="$DOWNLOAD_DIR/slimference_${ver}_darwin_${arch}/slimference"
  if [[ ! -x "$FROM_BINARY" ]]; then
    echo "Release archive did not contain an executable slimference binary." >&2
    exit 1
  fi
}

install_binary() {
  local src="$1"
  if [[ ! -f "$src" ]]; then
    echo "Binary not found: $src" >&2
    exit 1
  fi
  local tmp
  tmp="$(mktemp "$INSTALL_DIR/.slimference.tmp.XXXXXX")"
  cp "$src" "$tmp"
  chmod 0755 "$tmp"
  mv "$tmp" "$BIN_PATH"
}

if [[ "$VERIFY_ONLY" -eq 1 ]]; then
  verify_install
  exit 0
fi

mkdir -p "$INSTALL_DIR"

if [[ "$SCRIPT_ON_DISK" -eq 1 && -z "$FROM_BINARY" && -x "$SOURCE_DIR/slimference" && ! -d "$SOURCE_DIR/scripts/build" ]]; then
  FROM_BINARY="$SOURCE_DIR/slimference"
fi

if [[ -z "$FROM_BINARY" && ! -d "$SOURCE_DIR/scripts/build" ]]; then
  download_release_binary
fi

if [[ -n "$FROM_BINARY" ]]; then
  install_binary "$FROM_BINARY"
else
  if [[ ! -d "$SOURCE_DIR/scripts/build" ]]; then
    echo "Source build files are missing and no release binary was found. Pass --from-binary PATH." >&2
    exit 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build from source. Install Go 1.25+ or pass --from-binary PATH." >&2
    exit 1
  fi
  BUILD_TMP="$(mktemp "$INSTALL_DIR/.slimference.build.XXXXXX")"
  (cd "$SOURCE_DIR" && go run ./scripts/build --version "$VERSION" --out "$BUILD_TMP")
  install_binary "$BUILD_TMP"
fi

echo
echo "Installed Slimference:"
"$BIN_PATH" --version

ensure_path_hint

cat <<NEXT

Next steps:
  slimference

Optional setup from the TUI:
  Open Setup to install the scoped Codex service/hooks.

CLI setup equivalent:
  "$BIN_PATH" install
  "$BIN_PATH" status --preflight

Normal Codex launches stay direct unless started through Slimference.
NEXT
