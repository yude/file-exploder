import Foundation

/// OpenSSH connection multiplexing for one `SSHConnection`.
///
/// Every command this client runs is its own `/usr/bin/ssh` invocation, and
/// each one paid a full TCP connect, key exchange and authentication before it
/// could say a word. That handshake is the dominant cost of nearly everything
/// the app does: each directory listing, each 300ms poll while a job runs, the
/// background refresh on every open window, and each of the eight concurrent
/// lookups a bulk operation resolves its outcomes with.
///
/// `ControlMaster` has the first invocation open a master connection that
/// every later one attaches to as a new channel, so only the first pays the
/// handshake. Measured against a local sshd - where a handshake is at its
/// cheapest, with no network round trips to amortise at all - twenty
/// sequential commands went from 3.8s to 0.64s, and eight concurrent ones from
/// serialising behind each other's handshake to 0.15s in total. Over a real
/// link the difference is larger still: a handshake is several round trips
/// where an attached channel is one.
///
/// The socket belongs to a single SSHConnection instance rather than being
/// shared by target host. Keying it on the target's identity would mean
/// hashing that identity into a filename, and two different servers colliding
/// on that name would quietly send one host's commands to the other. A
/// per-instance random name cannot collide, and an instance already backs
/// exactly one window - the scope over which the reuse is wanted.
enum SSHControlSocket {
    /// macOS caps a unix-domain socket path at 104 bytes (`sun_path`); Linux
    /// allows 108.
    ///
    /// OpenSSH does not fall back to an ordinary connection when the
    /// ControlPath it is handed is too long - it refuses to connect at all,
    /// with "ControlPath too long" - so an over-long path would break every
    /// command outright rather than merely leave them slow. The length is
    /// therefore decided here, and multiplexing left off entirely when it does
    /// not fit. 100 keeps margin for the terminating NUL and for the bound
    /// differing between platforms.
    static let maxPathLength = 100

    /// Created under the user's own temporary directory, which macOS already
    /// gives each user privately.
    static let directoryName = "file-exploder"

    /// How long the master lingers after its last command. Long enough that a
    /// window refreshing in the background (the interval is capped at 300s)
    /// keeps reusing one connection, short enough that a window that never got
    /// to close its master cleanly does not hold one open indefinitely.
    static let persistSeconds = 300

    /// Joins `directory` and `name`, or nil when the result cannot serve as a
    /// control socket. Kept pure so the rules are testable without touching
    /// the filesystem.
    static func path(inDirectory directory: String, name: String) -> String? {
        let base = directory.hasSuffix("/") ? String(directory.dropLast()) : directory
        let candidate = base + "/" + name
        guard candidate.utf8.count < maxPathLength else { return nil }
        // ssh expands %-tokens inside a ControlPath (%h for the host, %p for
        // the port, and so on), so a literal % anywhere in the path - by way
        // of an unusual TMPDIR - would silently produce a socket at some other
        // name than the one being reasoned about here. Nothing is lost by
        // declining: the caller runs unmultiplexed, exactly as before.
        guard !candidate.contains("%") else { return nil }
        return candidate
    }

    /// A socket name unique to one connection. 16 hex characters keeps the
    /// path short while leaving a collision between two live windows out of
    /// the question.
    static func newName() -> String {
        let hex = UUID().uuidString.replacingOccurrences(of: "-", with: "")
        return String(hex.prefix(16)).lowercased()
    }

    /// Prepares a control socket path for one connection, creating the
    /// directory it lives in.
    ///
    /// Returns nil - meaning "run every command on its own connection, exactly
    /// as before multiplexing existed" - if the directory cannot be created or
    /// the path would not serve as a socket. Multiplexing is an optimisation,
    /// so every way it can be unavailable degrades to the previous behaviour
    /// rather than failing.
    static func prepare() -> String? {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent(directoryName, isDirectory: true)
        do {
            try FileManager.default.createDirectory(
                at: directory,
                withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700]
            )
        } catch {
            return nil
        }
        return path(inDirectory: directory.path, name: newName())
    }
}
