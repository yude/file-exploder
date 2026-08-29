using System.Text.Json;

namespace FileExploder.Services;

/// App-wide user preferences, persisted to disk and shared by every window
/// open in this process - the equivalent of the macOS client's
/// @AppStorage-backed settings, which all read from the same UserDefaults
/// domain no matter how many windows are open.
public static class AppSettings
{
    private sealed record StoredSettings
    {
        public bool ShowHiddenFiles { get; init; }
        public double RefreshInterval { get; init; } = DefaultRefreshInterval;
    }

    private const double DefaultRefreshInterval = 5.0;

    private static readonly Lock Gate = new();
    private static string _filePath = Path.Combine(AppPaths.DataDirectory, "settings.json");
    private static StoredSettings? _cached;

    /// Test seam: points the store at a throwaway file instead of the real,
    /// shared %APPDATA% one, so tests never read or clobber this machine's
    /// actual saved preferences and don't leak state between test runs.
    internal static void UseFileForTesting(string path)
    {
        lock (Gate)
        {
            _filePath = path;
            _cached = null;
        }
    }

    /// Raised after any window changes a setting, so every other open
    /// window's FileListViewModel can react - the equivalent of observing
    /// UserDefaults.didChangeNotification.
    public static event Action? Changed;

    public static bool ShowHiddenFiles
    {
        get => Load().ShowHiddenFiles;
        set => Mutate(s => s with { ShowHiddenFiles = value });
    }

    public static double RefreshInterval
    {
        get => Load().RefreshInterval;
        set => Mutate(s => s with { RefreshInterval = value });
    }

    private static StoredSettings Load()
    {
        lock (Gate)
        {
            if (_cached is { } cached)
            {
                return cached;
            }
            _cached = ReadFromDisk();
            return _cached;
        }
    }

    private static StoredSettings ReadFromDisk()
    {
        try
        {
            var json = File.ReadAllText(_filePath);
            return JsonSerializer.Deserialize<StoredSettings>(json) ?? new StoredSettings();
        }
        catch (IOException)
        {
            return new StoredSettings();
        }
        catch (JsonException)
        {
            return new StoredSettings();
        }
    }

    private static void Mutate(Func<StoredSettings, StoredSettings> mutate)
    {
        lock (Gate)
        {
            _cached = mutate(_cached ?? ReadFromDisk());
            File.WriteAllText(_filePath, JsonSerializer.Serialize(_cached));
        }
        Changed?.Invoke();
    }
}
