using System.Text.Json;
using FileExploder.Models;
using FileExploder.Utilities;

namespace FileExploder.Services;

/// Persists the saved-server list, shared by every window in this process -
/// the same single UserDefaults-backed list every ConnectionViewModel window
/// on the macOS client reads and writes.
public static class SavedServersStore
{
    private static readonly Lock Gate = new();
    private static string _filePath = AppPaths.SavedServersFile;

    /// Raised after any window saves the list, so every other open window's
    /// ConnectionViewModel can reload it - the equivalent of observing
    /// UserDefaults.didChangeNotification for the saved-server key.
    public static event Action? Changed;

    /// Test seam: points the store at a throwaway file instead of the real,
    /// shared %APPDATA% one, so tests never read or clobber this machine's
    /// actual saved servers and don't leak state between test runs.
    internal static void UseFileForTesting(string path)
    {
        lock (Gate)
        {
            _filePath = path;
        }
    }

    /// Returns the persisted list, or throws JsonException when it is
    /// present but fundamentally unreadable (not merely one entry this
    /// version doesn't recognize - see LenientJson) - the caller should keep
    /// whatever it already has rather than treat that as "no servers".
    ///
    /// A missing file is not an error: it means nothing has been saved yet.
    public static List<Server> Load()
    {
        string json;
        lock (Gate)
        {
            try
            {
                json = File.ReadAllText(_filePath);
            }
            catch (IOException)
            {
                return [];
            }
        }
        return LenientJson.DeserializeLenientArray<Server>(json, JsonDefaults.SavedServers);
    }

    public static void Save(List<Server> servers)
    {
        var json = JsonSerializer.Serialize(servers, JsonDefaults.SavedServers);
        lock (Gate)
        {
            AtomicFile.WriteAllText(_filePath, json);
        }
        Changed?.Invoke();
    }
}
