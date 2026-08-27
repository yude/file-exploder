#!/bin/bash
set -euo pipefail

echo "Building file-exploder for macOS..."
swift build -c release

APP_NAME="file-exploder.app"
CONTENTS_DIR="$APP_NAME/Contents"
MACOS_DIR="$CONTENTS_DIR/MacOS"
RESOURCES_DIR="$CONTENTS_DIR/Resources"

echo "Creating App Bundle..."
rm -rf "$APP_NAME"
mkdir -p "$MACOS_DIR"
mkdir -p "$RESOURCES_DIR"

# Copy executable
cp .build/release/file-exploder "$MACOS_DIR/"

# Create Info.plist
cat <<PLIST > "$CONTENTS_DIR/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>file-exploder</string>
    <key>CFBundleIdentifier</key>
    <string>com.yude.file-exploder</string>
    <key>CFBundleName</key>
    <string>file-exploder</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

echo "Signing App Bundle..."
codesign --force --deep -s - "$APP_NAME"

echo "App bundle created at: $PWD/$APP_NAME"
echo "You can move this to /Applications with:"
echo "  cp -r $APP_NAME /Applications/"
