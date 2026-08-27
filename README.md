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

The installer must be run as the same Linux user used by the macOS client's
SSH connection. It installs the binary in `~/.local/bin` and a per-user systemd
service, so the CLI and daemon share both filesystem permissions and the same
queue database. Do not run it with `sudo`.

To keep the service running after that user logs out, an administrator can run:

```bash
sudo loginctl enable-linger <ssh-user>
```

Check it with `systemctl --user status file-exploder`. The client's configured
remote root is a navigation boundary, not an operating-system sandbox; the
daemon has exactly the access rights of the SSH user.

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
