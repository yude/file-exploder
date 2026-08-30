namespace FileExploder.Services;

/// This app is a WinExe: it has no console, so an unhandled exception
/// otherwise vanishes with nothing more than Windows' generic "this program
/// has stopped working" (or, worse, a silently broken UI if the exception
/// escaped an async event handler Avalonia's dispatcher just logs and
/// swallows). Every unhandled-exception hook .NET exposes is wired to this
/// in Program.cs/App.axaml.cs, so a crash or a swallowed exception at least
/// leaves a trace to look at afterward.
public static class CrashLog
{
    private static string? _filePathOverride;

    public static string FilePath => _filePathOverride ?? Path.Combine(AppPaths.DataDirectory, "crash.log");

    /// Test seam: points the log at a throwaway file instead of the real,
    /// shared %APPDATA% one.
    internal static void UseFileForTesting(string path) => _filePathOverride = path;

    public static void Record(string source, Exception exception)
    {
        try
        {
            var entry = $"[{DateTimeOffset.Now:yyyy-MM-dd HH:mm:ss zzz}] {source}\n{exception}\n\n";
            File.AppendAllText(FilePath, entry);
        }
        catch (Exception)
        {
            // The log itself is best-effort - if the data directory is
            // unwritable, there is nowhere else to report that either.
        }
    }
}
