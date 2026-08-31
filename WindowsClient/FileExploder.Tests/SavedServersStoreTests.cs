using FileExploder.Models;
using FileExploder.Services;

namespace FileExploder.Tests;

/// Shares the "Local SSH" collection so this never runs concurrently with
/// another test also redirecting SavedServersStore's process-wide static
/// state onto a throwaway file.
[Collection("Local SSH")]
public sealed class SavedServersStoreTests : IDisposable
{
    private readonly string _file = Path.GetTempFileName();

    public SavedServersStoreTests()
    {
        File.Delete(_file); // the store should tolerate a file that doesn't exist yet
        SavedServersStore.UseFileForTesting(_file);
    }

    public void Dispose()
    {
        if (File.Exists(_file))
        {
            File.Delete(_file);
        }
    }

    private static Server NewServer(string name = "test") => new()
    {
        Name = name,
        Hostname = "example.test",
        Username = "someone",
    };

    [Fact]
    public void LoadingBeforeAnythingIsSavedReturnsAnEmptyList()
    {
        Assert.Empty(SavedServersStore.Load());
    }

    [Fact]
    public void SavedServersRoundTripThroughLoad()
    {
        var server = NewServer();
        SavedServersStore.Save([server]);

        var loaded = Assert.Single(SavedServersStore.Load());
        Assert.Equal(server, loaded);
    }

    [Fact]
    public void SavingRaisesChanged()
    {
        var raised = 0;
        void Handler() => raised++;
        SavedServersStore.Changed += Handler;
        try
        {
            SavedServersStore.Save([NewServer()]);
        }
        finally
        {
            SavedServersStore.Changed -= Handler;
        }

        Assert.Equal(1, raised);
    }

    [Fact]
    public void AnUnparsableFileThrowsInsteadOfSilentlyReturningEmpty()
    {
        File.WriteAllText(_file, "not json");

        Assert.ThrowsAny<System.Text.Json.JsonException>(() => SavedServersStore.Load());
    }

    [Fact]
    public void AnEntryThisVersionDoesNotRecognizeIsDroppedNotFatal()
    {
        // A well-formed JSON array where one element has an authType this
        // client build doesn't recognize - LenientJson should drop only
        // that element, not fail the whole load.
        File.WriteAllText(_file, """
            [
                {"id":"11111111-1111-1111-1111-111111111111","name":"good","hostname":"h","port":22,"username":"u","authType":"sshKey","remoteRoot":"/"},
                {"id":"22222222-2222-2222-2222-222222222222","name":"bad","hostname":"h","port":22,"username":"u","authType":"somethingNew","remoteRoot":"/"}
            ]
            """);

        var loaded = Assert.Single(SavedServersStore.Load());
        Assert.Equal("good", loaded.Name);
    }

    /// A servers.json that parses but is not an array - hand-edited, or
    /// corrupted into something still syntactically valid - must be reported
    /// as an unreadable server list, the same as any other unreadable file.
    /// EnumerateArray's own InvalidOperationException is not a JsonException
    /// and would sail past the one place that reports this to the user.
    [Theory]
    [InlineData("{}")]
    [InlineData("null")]
    [InlineData("\"a string\"")]
    [InlineData("42")]
    public void AFileThatIsNotAnArrayIsReportedAsUnreadable(string contents)
    {
        File.WriteAllText(_file, contents);
        Assert.ThrowsAny<System.Text.Json.JsonException>(() => SavedServersStore.Load());
    }
}
