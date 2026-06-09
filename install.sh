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
SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<USAGE
Slimference ${VERSION} installer

Usage:
  ./install.sh [--from-binary PATH] [--no-path-hint] [--verify-only]

Options:
  --from-binary PATH   Install an existing slimference binary instead of building
                       from this source checkout.
  --no-path-hint       Do not print shell profile guidance.
  --verify-only        Only verify the installed binary and preflight status.
  -h, --help           Show this help.

Repository:
  ${REPO_URL}
USAGE
}

FROM_BINARY=""
PATH_HINT=1
VERIFY_ONLY=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from-binary)
      [[ $# -ge 2 ]] || { echo "--from-binary requires a path" >&2; exit 2; }
      FROM_BINARY="$2"
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

if [[ "$VERIFY_ONLY" -eq 1 ]]; then
  verify_install
  exit 0
fi

mkdir -p "$INSTALL_DIR"

if [[ -z "$FROM_BINARY" && -x "$SOURCE_DIR/slimference" && ! -d "$SOURCE_DIR/scripts/build" ]]; then
  FROM_BINARY="$SOURCE_DIR/slimference"
fi

if [[ -n "$FROM_BINARY" ]]; then
  if [[ ! -f "$FROM_BINARY" ]]; then
    echo "Binary not found: $FROM_BINARY" >&2
    exit 1
  fi
  tmp="$(mktemp "$INSTALL_DIR/.slimference.tmp.XXXXXX")"
  cp "$FROM_BINARY" "$tmp"
  chmod 0755 "$tmp"
  mv "$tmp" "$BIN_PATH"
else
  if [[ ! -d "$SOURCE_DIR/scripts/build" ]]; then
    echo "Source build files are missing and no release binary was found. Pass --from-binary PATH." >&2
    exit 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build from source. Install Go 1.25+ or pass --from-binary PATH." >&2
    exit 1
  fi
  (cd "$SOURCE_DIR" && go run ./scripts/build --version "$VERSION" --install)
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
