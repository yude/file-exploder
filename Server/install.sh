#!/bin/bash
set -eu

echo "=== FileExploder Server Installer ==="
echo ""

# The queue and daemon deliberately run as the SSH login user so that both see
# the same database and have exactly that user's filesystem permissions.
if [ "$EUID" -eq 0 ]; then
    echo "Do not run this installer as root." >&2
    echo "Run it as the SSH user that will use file-exploder." >&2
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build file-exploder but was not found in PATH." >&2
    echo "Install it from https://go.dev/dl/ and re-run this script." >&2
    exit 1
fi

if ! systemctl --user show-environment >/dev/null 2>&1; then
    echo "No systemd user session is available for $(whoami)." >&2
    echo "Log in over SSH as this user (or ask an administrator to run" >&2
    echo "'loginctl enable-linger $(whoami)') and re-run this script." >&2
    exit 1
fi

INSTALL_DIR="$HOME/.local/bin"
SERVICE_DIR="$HOME/.config/systemd/user"
mkdir -p "$INSTALL_DIR"
mkdir -p "$SERVICE_DIR"

echo "Building file-exploder..."
cd "$(dirname "$0")"
BUILD_OUTPUT="$(mktemp)"
trap 'rm -f "$BUILD_OUTPUT"' EXIT
go build -buildvcs=false -o "$BUILD_OUTPUT" .

echo "Installing to $INSTALL_DIR/file-exploder"
systemctl --user stop file-exploder 2>/dev/null || true
install -m 0755 "$BUILD_OUTPUT" "$INSTALL_DIR/file-exploder"

echo "Installing systemd service..."
install -m 0644 file-exploder.service "$SERVICE_DIR/file-exploder.service"

systemctl --user daemon-reload
systemctl --user enable file-exploder
systemctl --user start file-exploder
echo "Service started. Check status: systemctl --user status file-exploder"
echo ""
echo "To keep the user service running after logout, an administrator can run:"
echo "  loginctl enable-linger $(whoami)"

echo ""
echo "Installation complete!"
echo "Usage: file-exploder add --type rename --src /path/a --dst /path/b"
