using System.Text.Json;

namespace FileExploder.Services;

/// Trust-on-first-use host key pinning, mirroring the macOS client's use of
/// OpenSSH's `StrictHostKeyChecking=accept-new`: the first connection to a
/// given host:port records its key fingerprint, and every later connection
/// is only accepted if the fingerprint still matches - protecting against a
/// man-in-the-middle after that first, unverified connection, the same way
/// a regular `known_hosts` file does.
public sealed class KnownHostsStore
{
    private readonly string _path;
    private readonly Lock _lock = new();
    private Dictionary<string, string> _fingerprintsByHost;

    public KnownHostsStore() : this(AppPaths.KnownHostsFile)
    {
    }

    public KnownHostsStore(string path)
    {
        _path = path;
        _fingerprintsByHost = Load(path);
    }

    private static Dictionary<string, string> Load(string path)
    {
        try
        {
            var json = File.ReadAllText(path);
            return JsonSerializer.Deserialize<Dictionary<string, string>>(json) ?? [];
        }
        catch (IOException)
        {
            return [];
        }
        catch (JsonException)
        {
            return [];
        }
    }

    private void Save()
    {
        var json = JsonSerializer.Serialize(_fingerprintsByHost, new JsonSerializerOptions { WriteIndented = true });
        File.WriteAllText(_path, json);
    }

    private static string KeyFor(string host, int port) => $"{host}:{port}";

    /// Returns true if `fingerprint` is trusted for `host:port` - either
    /// because it was already recorded and matches, or because this is the
    /// first time this host:port has been seen (and it is recorded now).
    /// Returns false only when a *different* fingerprint was already on
    /// file for this host:port.
    public bool TrustAndRecord(string host, int port, string fingerprint)
    {
        var key = KeyFor(host, port);
        lock (_lock)
        {
            if (_fingerprintsByHost.TryGetValue(key, out var existing))
            {
                return existing == fingerprint;
            }
            _fingerprintsByHost[key] = fingerprint;
            Save();
            return true;
        }
    }
}
