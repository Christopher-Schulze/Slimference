#!/usr/bin/env bash
# install.sh - wire the slimference user-scoped systemd unit on Linux.
# Idempotent: safe to re-run after a binary update.
#
# Usage:
#   ./scripts/service/linux/install.sh
#
# Exceptions to the TS-only tooling rule: this script must run before
# any Bun/Go toolchain is guaranteed on a fresh Linux host, so it lives
# as a minimal bash entry point.

set -euo pipefail

UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT_NAME="slimference.service"
SRC_UNIT="$(dirname "$0")/$UNIT_NAME"

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl not found - this installer supports Linux with systemd only." >&2
  echo "macOS users run 'slimference service install' instead." >&2
  exit 1
fi

mkdir -p "$UNIT_DIR"
install -m 0644 "$SRC_UNIT" "$UNIT_DIR/"

systemctl --user daemon-reload
systemctl --user enable --now "$UNIT_NAME"

echo
echo "Slimference is now running under systemd --user. Next steps:"
echo "  status:  systemctl --user status $UNIT_NAME"
echo "  logs:    journalctl --user -u $UNIT_NAME -f"
echo "  stop:    systemctl --user stop $UNIT_NAME"
echo "  remove:  systemctl --user disable --now $UNIT_NAME && rm $UNIT_DIR/$UNIT_NAME"
echo
echo "For the daemon to survive logout enable lingering once:"
echo "  loginctl enable-linger \$USER"
