using System.Text.Json;
using System.Text.Json.Serialization;

namespace FileExploder.Models;

/// A saved SSH connection profile.
///
/// Immutable by design (an `init`-only record, matching a Swift value-type
/// struct): editing a saved server means building a new value with the same
/// Id, not mutating a shared instance another window might be observing at
/// the same time.
public sealed record Server
{
    public const ushort DefaultPort = 22;

    public Guid Id { get; init; } = Guid.NewGuid();
    public string Name { get; init; } = "";
    public string Hostname { get; init; } = "";
    public ushort Port { get; init; } = DefaultPort;
    public string Username { get; init; } = "";
    public AuthType AuthType { get; init; } = AuthType.SshKey;
    public string? KeyPath { get; init; }
    public string RemoteRoot { get; init; } = "/";
}

[JsonConverter(typeof(AuthTypeJsonConverter))]
public enum AuthType
{
    SshKey,
}

public static class AuthTypeExtensions
{
    /// Deliberately not the wire value: the wire value is what lands in the
    /// saved server list, so spelling the label there would mean any change
    /// to the wording - a translation, a clearer term, a second auth method
    /// named in the same style - silently made every stored server
    /// undecodable, and the list would come back empty.
    public static string DisplayName(this AuthType type) => type switch
    {
        AuthType.SshKey => "SSHキー",
        _ => throw new ArgumentOutOfRangeException(nameof(type), type, message: null),
    };
}

/// Encodes as the stable wire value ("sshKey"), and decodes either that or -
/// so lists written by an earlier version still load - the display label
/// this used to be stored as ("SSHキー"). Any other value is rejected rather
/// than silently mapped to a default.
public sealed class AuthTypeJsonConverter : JsonConverter<AuthType>
{
    private const string WireValue = "sshKey";
    private const string LegacyLabel = "SSHキー";

    public override AuthType Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        var raw = reader.GetString();
        return raw switch
        {
            WireValue or LegacyLabel => AuthType.SshKey,
            _ => throw new JsonException($"Unknown authentication type: {raw}"),
        };
    }

    public override void Write(Utf8JsonWriter writer, AuthType value, JsonSerializerOptions options)
    {
        writer.WriteStringValue(WireValue);
    }
}
