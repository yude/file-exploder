# file-exploder

A native macOS SSH remote file manager with server-side operation queuing.

## Features

- **Native macOS UI** — Built with SwiftUI, feels like Finder
- **SSH** — Connect to any Linux server via SSH
- **Finder-like Interface** — List view with sorting, searching, and multi-selection
- **Server-Side Queue** — File operations continue even after client disconnect
- **Persistent Operations** — Queue survives client disconnection
- **Multiple Operations** — Rename, move, delete, copy, mkdir, chmod
- **Symlink Aware** — Links are shown as links, and symlinked directories can be
  browsed like ordinary ones

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
SSH connection, so the CLI and daemon share both filesystem permissions and
the same queue database. Go 1.26.7 or newer is required to avoid known
standard-library vulnerabilities.

- As a regular SSH user (recommended), it installs to `~/.local/bin` and
  creates a per-user systemd service.
- As `root`, it installs to `/usr/local/bin` and creates the system service
  `/etc/systemd/system/file-exploder.service`. Use this mode only when the
  macOS client also connects as `root`. The daemon then has unrestricted access
  to the server filesystem, and its queue lives under `/root/.file-exploder`.

To keep the service running after that user logs out, an administrator can run:

```bash
sudo loginctl enable-linger <ssh-user>
```

Check a regular-user installation with
`systemctl --user status file-exploder`, or a root installation with
`systemctl status file-exploder`. The client's configured remote root is a
navigation boundary, not an operating-system sandbox; the daemon has exactly
the access rights of the SSH user.

Every command writes JSON to stdout and errors to stderr, so the client can
parse results without screen-scraping. Queue state lives in
`~/.file-exploder/queue.db`; set `FILE_EXPLODER_DATA_DIR` to an absolute path to
move the database, log, and daemon lock elsewhere.

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
file-exploder add --type copy --src /path/a --dst /path/newdir/a
file-exploder add --type delete --src /path/file
file-exploder add --type mkdir --dst /path/newdir
file-exploder add --type chmod --dst /path/file --mode 755

# Check queue status (all pending and running jobs, or one job by id)
file-exploder status
file-exploder status <job-id>

# Cancel a job. Only pending jobs can be cancelled: once the daemon has
# started acting on the files there is nothing safe to roll back to.
file-exploder cancel <job-id>

# View recent finished jobs
file-exploder log

# Inspect the filesystem as JSON (used by the macOS client)
file-exploder list /path/to/dir
file-exploder stat /path/to/file

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
