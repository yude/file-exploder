import Foundation

/// Lexical path handling for paths that live on the *remote* host.
///
/// `NSString.standardizingPath` resolves symlinks against the *local*
/// filesystem, which on macOS rewrites `/tmp`, `/var` and `/etc` to their
/// `/private/...` targets. Applied to a remote path that yields something the
/// server has never heard of, so remote paths are normalised purely lexically
/// here — the same thing `filepath.Clean` does on the server side.
enum RemotePath {
    /// Collapses `.`, `..`, duplicate separators and any trailing separator.
    /// Relative input is returned untouched: callers reject it separately, and
    /// silently rooting it would invent a path the user never asked for.
    static func standardized(_ path: String) -> String {
        guard path.hasPrefix("/") else { return path }

        var components: [String] = []
        for component in splitComponents(path) {
            switch component {
            case ".":
                continue
            case "..":
                if !components.isEmpty {
                    components.removeLast()
                }
            default:
                components.append(component)
            }
        }
        return "/" + components.joined(separator: "/")
    }

    static func parent(of path: String) -> String {
        var components = splitComponents(standardized(path))
        guard !components.isEmpty else { return "/" }
        components.removeLast()
        return "/" + components.joined(separator: "/")
    }

    /// Splits a path into components over unicode scalars, not Characters: a
    /// separator immediately followed by a combining mark forms a single
    /// Character that does not compare equal to "/", so Character-based
    /// `split(separator: "/")` silently swallowed such a separator into
    /// whichever component preceded it instead of treating it as a boundary
    /// (see isValidComponent's own note on the same hazard). Empty
    /// components - leading, trailing or duplicated separators - are
    /// omitted, matching `split(separator:)`'s default behaviour.
    private static func splitComponents(_ path: String) -> [String] {
        var result: [String] = []
        var current = ""
        for scalar in path.unicodeScalars {
            if scalar == "/" {
                if !current.isEmpty {
                    result.append(current)
                    current = ""
                }
            } else {
                current.unicodeScalars.append(scalar)
            }
        }
        if !current.isEmpty {
            result.append(current)
        }
        return result
    }

    static func appending(_ name: String, to path: String) -> String {
        let base = standardized(path)
        return base == "/" ? "/" + name : base + "/" + name
    }

    /// Whether `name` can stand as a single path component.
    ///
    /// The separator check runs over unicode scalars, not Characters: Swift
    /// groups a separator followed by a combining mark into one Character that
    /// does not compare equal to "/", so `name.contains("/")` waved through
    /// names carrying an embedded separator and the caller went on to build a
    /// path with more components than it meant to.
    static func isValidComponent(_ name: String) -> Bool {
        guard !name.isEmpty, name != ".", name != ".." else { return false }
        return !name.unicodeScalars.contains { $0 == "/" || $0 == "\0" }
    }

    /// Whether `path` could be moved into `destination`.
    ///
    /// Rejects the two drops the server would only fail on: the entry is
    /// already in that directory, and a directory dropped onto itself or into
    /// its own subtree. Refusing them here means the drag is declined while the
    /// user can still see what they aimed at, instead of a job appearing in the
    /// queue and failing a moment later.
    static func canMove(_ path: String, into destination: String) -> Bool {
        let target = standardized(destination)
        if parent(of: path) == target {
            return false
        }
        return !isDescendant(target, of: path)
    }

    /// Whether `path` is `root` or sits underneath it. Both sides are
    /// standardized first so `/srv/data/../data/x` is recognised as inside
    /// `/srv/data`.
    static func isDescendant(_ path: String, of root: String) -> Bool {
        let root = standardized(root)
        let target = standardized(path)
        guard root.hasPrefix("/"), target.hasPrefix("/") else { return false }
        if root == "/" { return true }
        return target == root || target.hasPrefix(root + "/")
    }
}
