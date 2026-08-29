using System.Text.Json;

namespace FileExploder.Utilities;

/// The single place the app's two distinct JSON dialects are configured.
public static class JsonDefaults
{
    /// For the locally-persisted saved-server list. QueueJob/RemoteFile use
    /// explicit [JsonPropertyName] attributes to match the Go server's own
    /// snake_case wire format instead, since that format isn't this app's to
    /// choose - Server has no such external contract, so this just picks one
    /// convention (camelCase) and applies it consistently.
    public static readonly JsonSerializerOptions SavedServers = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        WriteIndented = true,
    };
}
