using System.Text;

namespace FileExploder.Utilities;

public static class StringExtensions
{
    /// Escapes a string to be safely used in a Unix shell command (the
    /// remote side is always Linux, regardless of what this client runs on).
    /// Wraps the string in single quotes and escapes internal single quotes.
    public static string ShellEscaped(this string value)
    {
        var escaped = value.Replace("'", "'\\''");
        return "'" + escaped + "'";
    }

    /// ASCII-only transport for remote paths, matching the Go server's
    /// --path-base64/--src-base64/--dst-base64 flags. On Windows, .NET
    /// strings are already UTF-16 code points with no macOS-style Unicode
    /// re-normalization on the way through Process argument passing, so this
    /// is not defending against the exact hazard the Swift client's own
    /// comment describes - but routing through the same base64 flags the
    /// server already expects keeps both clients' wire behavior identical
    /// and sidesteps shell-quoting a path entirely.
    public static string Utf8Base64(this string value) =>
        Convert.ToBase64String(Encoding.UTF8.GetBytes(value));
}
