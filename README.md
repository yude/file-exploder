# RemoteFS (file-exploder)

A native macOS SSH remote file manager with server-side operation queuing.

## Features

- **Native macOS UI** — Built with SwiftUI, feels like Finder
- **SSH** — Connect to any Linux server via SSH
- **Finder-like Interface** — List view with sorting, searching, and multi-selection
- **Server-Side Queue** — File operations continue even after client disconnect
- **Persistent Operations** — Queue survives client disconnection
- **Multiple Operations** — Rename, move, delete, copy, mkdir, chmod

## Architecture

```
┌─────────────────────────────────────────┐
│           macOS Client (Swift)          │
│  SwiftUI + SSH exec commands            │
└─────────────────┬───────────────────────┘
                  │ SSH
                  ▼
┌─────────────────────────────────────────┐
│           Linux Server (Go)             │
│  file-exploder daemon + SQLite queue        │
└─────────────────────────────────────────┘
```

## Installation

### Server (Linux)

```bash
cd Server
chmod +x install.sh
./install.sh
```

Or manually:

```bash
cd Server
go build -buildvcs=false -o file-exploder .
sudo cp file-exploder /usr/local/bin/
sudo cp file-exploder.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable file-exploder
sudo systemctl start file-exploder
```

### Client (macOS)

```bash
cd Client
./build_mac.sh
cp -r file-exploder.app /Applications/
```

Or open `Client/Package.swift` in Xcode and build (Command+B).

## Usage

### SSH Commands

```bash
# Add operation to queue
file-exploder add --type rename --src /path/a --dst /path/b
file-exploder add --type move --src /path/a --dst /path/newdir/a
file-exploder add --type delete --src /path/file
file-exploder add --type mkdir --dst /path/newdir
file-exploder add --type chmod --dst /path/file --mode 755

# Check queue status
file-exploder status

# Cancel a job
file-exploder cancel <job-id>

# View logs
file-exploder log

# Start daemon
file-exploder daemon
```

### macOS Client

1. Launch file-exploder
2. Click "+" to add a server connection
3. Enter server details (hostname, username, SSH key path)
4. Double-click server to connect
5. Browse and manage files

## Development

### Server

```bash
cd Server
go mod tidy
go build -buildvcs=false -o file-exploder .
```

### Client

```bash
cd Client
swift build
```

## License

MIT
