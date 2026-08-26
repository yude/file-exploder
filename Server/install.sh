#!/bin/bash
set -e

echo "=== FileExploder Server Installer ==="
echo ""

# Check if running as root for system-wide install
if [ "$EUID" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
    SERVICE_DIR="/etc/systemd/system"
else
    INSTALL_DIR="$HOME/.local/bin"
    SERVICE_DIR="$HOME/.config/systemd/user"
    mkdir -p "$INSTALL_DIR"
    mkdir -p "$SERVICE_DIR"
fi

# Build
echo "Building file-exploder..."
cd "$(dirname "$0")"
go build -buildvcs=false -o file-exploder .

# Install binary
echo "Installing to $INSTALL_DIR/file-exploder"
cp file-exploder "$INSTALL_DIR/file-exploder"
chmod +x "$INSTALL_DIR/file-exploder"

# Install service
echo "Installing systemd service..."
if [ "$EUID" -eq 0 ]; then
    cp file-exploder.service "$SERVICE_DIR/file-exploder.service"
else
    # Replace /usr/local/bin with ~/.local/bin in user service
    sed "s|/usr/local/bin/file-exploder|$INSTALL_DIR/file-exploder|g" file-exploder.service > "$SERVICE_DIR/file-exploder.service"
fi

if [ "$EUID" -eq 0 ]; then
    systemctl daemon-reload
    systemctl enable file-exploder
    systemctl start file-exploder
    echo "Service started. Check status: systemctl status file-exploder"
else
    systemctl --user daemon-reload
    systemctl --user enable file-exploder
    systemctl --user start file-exploder
    echo "Service started. Check status: systemctl --user status file-exploder"
    echo ""
    echo "Note: To survive logout, run: loginctl enable-linger $(whoami)"
fi

echo ""
echo "Installation complete!"
echo "Usage: file-exploder add --type rename --src /path/a --dst /path/b"
