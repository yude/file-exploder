using System.Text.Json;
using FileExploder.Models;
using FileExploder.Utilities;

namespace FileExploder.Tests;

public class ServerAuthTypeTests
{
    private static AuthType Decode(string raw) =>
        JsonSerializer.Deserialize<AuthType>($"\"{raw}\"");

    /// The stored value must not be the label, or changing the wording
    /// would make every saved server undecodable.
    [Fact]
    public void StoresAStableValueRatherThanTheLabel()
    {
        var encoded = JsonSerializer.Serialize(AuthType.SshKey);
        Assert.Equal("\"sshKey\"", encoded);
        Assert.Equal("SSHキー", AuthType.SshKey.DisplayName());
    }

    [Fact]
    public void StillReadsListsSavedBeforeTheValueWasSeparated()
    {
        Assert.Equal(AuthType.SshKey, Decode("SSHキー"));
        Assert.Equal(AuthType.SshKey, Decode("sshKey"));
    }

    [Fact]
    public void RejectsSomethingItDoesNotRecognise()
    {
        Assert.Throws<JsonException>(() => Decode("password"));
    }

    [Fact]
    public void AWholeSavedServerRoundTrips()
    {
        var server = new Server
        {
            Name = "srv",
            Hostname = "example.test",
            Port = 2222,
            Username = "user",
            KeyPath = "/home/user/.ssh/id_ed25519",
            RemoteRoot = "/srv",
        };
        var restored = JsonSerializer.Deserialize<Server>(JsonSerializer.Serialize(server, JsonDefaults.SavedServers), JsonDefaults.SavedServers);
        Assert.Equal(server, restored);
    }
}

public class LenientJsonTests
{
    /// One entry in a saved-server list written by a different app version
    /// with an authType this one no longer (or does not yet) recognize must
    /// not blank out every other saved server.
    [Fact]
    public void DropsOnlyTheServerThatDoesNotDecode()
    {
        var json = """
        [
            {"id":"11111111-1111-1111-1111-111111111111","name":"a","hostname":"h","port":22,"username":"u","authType":"sshKey","remoteRoot":"/"},
            {"id":"22222222-2222-2222-2222-222222222222","name":"b","hostname":"h","port":22,"username":"u","authType":"unknown-method","remoteRoot":"/"}
        ]
        """;
        var servers = LenientJson.DeserializeLenientArray<Server>(json, JsonDefaults.SavedServers);
        Assert.Single(servers);
        Assert.Equal("a", servers[0].Name);
    }

    /// Likewise, one job with an operation type this client doesn't
    /// recognize - a newer server, a build skew - must not hide the rest of
    /// the queue or log response.
    [Fact]
    public void DropsOnlyTheQueueJobThatDoesNotDecode()
    {
        var json = """
        [
            {"id":"1","type":"delete","status":"completed","created_at":"2026-08-27T00:00:00Z"},
            {"id":"2","type":"beam-up","status":"completed","created_at":"2026-08-27T00:00:00Z"}
        ]
        """;
        var jobs = LenientJson.DeserializeLenientArray<QueueJob>(json, new JsonSerializerOptions());
        Assert.Single(jobs);
        Assert.Equal("1", jobs[0].Id);
    }
}
