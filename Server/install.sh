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
# Stop existing service if running so we can overwrite the binary
if [ "$EUID" -eq 0 ]; then
    systemctl stop file-exploder 2>/dev/null || true
else
    systemctl --user stop file-exploder 2>/dev/null || true
fi

cp file-exploder "$INSTALL_DIR/file-exploder"
chmod +x "$INSTALL_DIR/file-exploder"

# Install service
echo "Installing systemd service..."

if [ "$EUID" -eq 0 ]; then
    # Create system user for daemon and configure system-wide install
    useradd -r -s /bin/false file-exploder || true
    mkdir -p /var/lib/file-exploder
    chown file-exploder:file-exploder /var/lib/file-exploder
    chmod 0700 /var/lib/file-exploder
    
    # Configure daemon to use the specific data dir
    export FILE_EXPLODER_DATA_DIR="/var/lib/file-exploder"
    
    cp file-exploder.service "$SERVICE_DIR/file-exploder.service"
    
    # Change ExecStart, Add User= and Environment= 
    sed -i "s|ExecStart=.*|ExecStart=$INSTALL_DIR/file-exploder daemon|g" "$SERVICE_DIR/file-exploder.service"
    sed -i "/\[Service\]/a User=file-exploder" "$SERVICE_DIR/file-exploder.service"
    sed -i "/\[Service\]/a Environment=\"FILE_EXPLODER_DATA_DIR=/var/lib/file-exploder\"" "$SERVICE_DIR/file-exploder.service"
    
    systemctl daemon-reload
    systemctl enable file-exploder
    systemctl start file-exploder
    echo "Service started. Check status: systemctl status file-exploder"
else
    # User-level install
    # Replace /usr/local/bin with ~/.local/bin in user service
    sed "s|/usr/local/bin/file-exploder|$INSTALL_DIR/file-exploder|g" file-exploder.service > "$SERVICE_DIR/file-exploder.service"
    
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
