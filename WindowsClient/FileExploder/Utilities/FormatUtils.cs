using FileExploder.Models;

namespace FileExploder.Utilities;

public static class FormatUtils
{
    public static string FormattedSize(long bytes)
    {
        const long kb = 1_000;
        const long mb = kb * 1_000;
        const long gb = mb * 1_000;
        const long tb = gb * 1_000;

        if (bytes < kb)
        {
            return $"{bytes} bytes";
        }

        var (value, unit) = bytes switch
        {
            >= tb => (bytes / (double)tb, "TB"),
            >= gb => (bytes / (double)gb, "GB"),
            >= mb => (bytes / (double)mb, "MB"),
            _ => (bytes / (double)kb, "KB"),
        };

        // Roughly three significant digits, the way most file managers show
        // sizes: "1.2 GB", "45.3 MB", "999 KB".
        var decimals = value >= 100 ? 0 : value >= 10 ? 1 : 2;
        return $"{value.ToString("F" + decimals)} {unit}";
    }

    public static string FormattedDate(DateTimeOffset date) =>
        date.ToLocalTime().ToString("g");
}

public static class FileIcons
{
    /// A short glyph standing in for a proper icon set: this client has no
    /// bundled vector icon assets, so a Unicode symbol carries the same
    /// at-a-glance file-type information a real icon would.
    public static string Icon(RemoteFile file)
    {
        if (file.IsDirectory)
        {
            return "📁";
        }
        if (file.IsSymlink)
        {
            return "🔗";
        }

        var ext = System.IO.Path.GetExtension(file.Name).TrimStart('.').ToLowerInvariant();
        return ext switch
        {
            "jpg" or "jpeg" or "png" or "gif" or "svg" or "webp" or "bmp" or "tiff" => "🖼️",
            "pdf" => "📕",
            "doc" or "docx" => "📄",
            "xls" or "xlsx" => "📊",
            "ppt" or "pptx" => "📽️",
            "zip" or "tar" or "gz" or "7z" or "rar" or "bz2" or "xz" => "🗜️",
            "mp3" or "wav" or "aac" or "flac" or "ogg" or "m4a" => "🎵",
            "mp4" or "mov" or "avi" or "mkv" or "webm" or "flv" => "🎬",
            "txt" or "md" or "log" or "rtf" => "📝",
            "json" or "xml" or "plist" or "yaml" or "yml" or "toml" => "🗒️",
            "sh" or "bash" or "zsh" or "fish" => "💻",
            "py" or "rb" or "js" or "ts" or "swift" or "go" or "rs" or "java" or "c" or "cpp" or "h" => "🧩",
            "exe" or "app" or "dmg" or "deb" or "rpm" => "⚙️",
            "link" or "url" or "webloc" => "🔗",
            _ => "📄",
        };
    }
}
