import Foundation

struct FormatUtils {
    static func formattedSize(_ bytes: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
    }
    
    static func formattedDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }
    
    static func relativeDate(_ date: Date) -> String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: date, relativeTo: Date())
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
    
    static func color(for file: RemoteFile) -> String {
        if file.isDirectory {
            return "folderAccent"
        }
        
        let ext = (file.name as NSString).pathExtension.lowercased()
        
        switch ext {
        case "jpg", "jpeg", "png", "gif", "svg", "webp":
            return "purple"
        case "pdf":
            return "red"
        case "mp3", "wav", "aac", "flac":
            return "pink"
        case "mp4", "mov", "avi", "mkv":
            return "blue"
        case "zip", "tar", "gz":
            return "brown"
        case "py", "rb":
            return "green"
        case "js", "ts":
            return "yellow"
        case "swift":
            return "orange"
        default:
            return "gray"
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
}
