namespace FileExploder.Services;

/// This app is a WinExe: it has no console, so an unhandled exception
/// otherwise vanishes with nothing more than Windows' generic "this program
/// has stopped working" (or, worse, a silently broken UI if the exception
/// escaped an async event handler Avalonia's dispatcher just logs and
/// swallows), and there is nowhere for an ad-hoc Console.WriteLine to go
/// either. Every unhandled-exception hook .NET exposes is wired to
/// LogException in Program.cs/App.axaml.cs; LogTrace is for the rarer case
/// where the code runs to completion without throwing but still needs to
/// leave a record of what it decided, for a bug that only reproduces
/// somewhere this can't be run interactively.
public static class DiagnosticLog
{
    private static string? _filePathOverride;

    public static string FilePath => _filePathOverride ?? Path.Combine(AppPaths.DataDirectory, "diagnostic.log");

    /// Test seam: points the log at a throwaway file instead of the real,
    /// shared %APPDATA% one.
    internal static void UseFileForTesting(string path) => _filePathOverride = path;

    public static void LogException(string source, Exception exception) =>
        Append($"[{DateTimeOffset.Now:yyyy-MM-dd HH:mm:ss zzz}] {source}\n{exception}\n\n");

    public static void LogTrace(string message) =>
        Append($"[{DateTimeOffset.Now:yyyy-MM-dd HH:mm:ss zzz}] {message}\n");

    private static void Append(string entry)
    {
        try
        {
            File.AppendAllText(FilePath, entry);
        }
        catch (Exception)
        {
            // The log itself is best-effort - if the data directory is
            // unwritable, there is nowhere else to report that either.
        }
    }
}
