import Foundation

struct FormatUtils {
    /// Reused across every row: building a `DateFormatter` per call is costly
    /// enough to show up while scrolling a large directory.
    private static let dateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter
    }()

    /// Reused across every row for the same reason as dateFormatter above.
    private static let sizeFormatter: ByteCountFormatter = {
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        return formatter
    }()

    static func formattedSize(_ bytes: Int64) -> String {
        sizeFormatter.string(fromByteCount: bytes)
    }

    static func formattedDate(_ date: Date) -> String {
        dateFormatter.string(from: date)
    }
}

struct FileIcons {
    static func icon(for file: RemoteFile) -> String {
        if file.isDirectory {
            return "folder.fill"
        }
        if file.isSymlink {
            return "link"
        }

        let ext = (file.name as NSString).pathExtension.lowercased()
        
        switch ext {
        // Images
        case "jpg", "jpeg", "png", "gif", "svg", "webp", "bmp", "tiff":
            return "photo"
        // Documents
        case "pdf":
            return "doc.richtext"
        case "doc", "docx":
            return "doc"
        case "xls", "xlsx":
            return "tablecells"
        case "ppt", "pptx":
            return "rectangle.on.rectangle"
        // Archives
        case "zip", "tar", "gz", "7z", "rar", "bz2", "xz":
            return "archivebox"
        // Audio
        case "mp3", "wav", "aac", "flac", "ogg", "m4a":
            return "music.note"
        // Video
        case "mp4", "mov", "avi", "mkv", "webm", "flv":
            return "film"
        // Text
        case "txt", "md", "log", "rtf":
            return "doc.text"
        // Code
        case "json", "xml", "plist", "yaml", "yml", "toml":
            return "doc.plaintext"
        case "sh", "bash", "zsh", "fish":
            return "terminal"
        case "py", "rb", "js", "ts", "swift", "go", "rs", "java", "c", "cpp", "h":
            return "chevron.left.forwardslash.chevron.right"
        // Executables
        case "exe", "app", "dmg", "deb", "rpm":
            return "app"
        // Links
        case "link", "url", "webloc":
            return "link"
        default:
            return "doc"
        }
    }
}

extension String {
    /// Escapes a string to be safely used in a Unix shell command.
    /// Wraps the string in single quotes and escapes internal single quotes.
    var shellEscaped: String {
        let escaped = self.replacingOccurrences(of: "'", with: "'\\''")
        return "'\(escaped)'"
    }

    /// ASCII-only transport for remote paths. This prevents macOS process
    /// argument conversion from changing composed characters into an
    /// equivalent normalization that names a different file on Linux.
    var utf8Base64: String {
        Data(utf8).base64EncodedString()
    }
}
