using FileExploder.Views;

namespace FileExploder.Tests;

/// ConnectionWindow's Save-button validation, exercised directly against a
/// headless Window rather than through ShowAsync - these are pure field
/// validation rules and don't need a live connection or saved-server store.
/// Every test body runs through HeadlessApp.RunOnUiThread - see its doc
/// comment for why a plain HeadlessApp.EnsureInitialized() plus touching
/// Avalonia objects directly is not reliable across different [Fact]s.
[Collection("Local SSH")]
public sealed class ConnectionWindowTests
{
    /// The key path is local to whatever machine runs this client, unlike
    /// the remote root (always POSIX, since the server is always Linux).
    /// Requiring a leading '/' here - which a straight, unadjusted port of
    /// the macOS client's own (correct, for a macOS local path) validation
    /// would do - rejects every real Windows path, including whatever the
    /// "参照" file picker itself returns, leaving Save permanently disabled.
    [Theory]
    [InlineData(@"C:\Users\someone\.ssh\id_ed25519")]
    [InlineData(@"\\server\share\id_ed25519")]
    [InlineData("")]
    public void AWindowsStyleKeyPathDoesNotBlockSaving(string keyPath) => HeadlessApp.RunOnUiThread(() =>
    {
        var window = new ConnectionWindow();
        window.SetFieldsForTesting(
            name: "test",
            hostname: "example.test",
            port: "22",
            username: "someone",
            keyPath: keyPath,
            remoteRoot: "/");

        Assert.True(window.IsSaveEnabledForTesting);
        window.Close();
    });

    [Fact]
    public void ARelativeKeyPathBlocksSaving() => HeadlessApp.RunOnUiThread(() =>
    {
        var window = new ConnectionWindow();
        window.SetFieldsForTesting(
            name: "test",
            hostname: "example.test",
            port: "22",
            username: "someone",
            keyPath: "relative/id_ed25519",
            remoteRoot: "/");

        Assert.False(window.IsSaveEnabledForTesting);
        window.Close();
    });
}
