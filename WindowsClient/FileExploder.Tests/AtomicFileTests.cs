using FileExploder.Services;

namespace FileExploder.Tests;

/// The stores all keep one JSON document that is worthless truncated, and all
/// read an unparseable file as "nothing saved" - so a half-finished write
/// costs the user their saved servers, or silently drops every pinned host key
/// back to trust-on-first-use.
public sealed class AtomicFileTests : IDisposable
{
    private readonly string _directory = Directory.CreateTempSubdirectory("file-exploder-atomic-tests").FullName;

    public void Dispose() => Directory.Delete(_directory, recursive: true);

    private string Path(string name) => System.IO.Path.Combine(_directory, name);

    [Fact]
    public void WritesAFileThatDidNotExist()
    {
        var path = Path("new.json");
        AtomicFile.WriteAllText(path, "{\"a\":1}");
        Assert.Equal("{\"a\":1}", File.ReadAllText(path));
    }

    [Fact]
    public void ReplacesExistingContentsEntirely()
    {
        var path = Path("existing.json");
        AtomicFile.WriteAllText(path, "a-much-longer-original-document");
        AtomicFile.WriteAllText(path, "short");
        // Not "shortnger-original-document": the replacement is a new file
        // moved into place, never an overwrite of the old one in situ.
        Assert.Equal("short", File.ReadAllText(path));
    }

    /// UTF-8 with no byte-order mark, matching what File.WriteAllText produced
    /// before - a BOM would be a leading character every JSON reader here would
    /// then have to tolerate.
    [Fact]
    public void WritesUtf8WithoutAByteOrderMark()
    {
        var path = Path("unicode.json");
        AtomicFile.WriteAllText(path, "{\"name\":\"日本語\"}");
        var bytes = File.ReadAllBytes(path);
        Assert.False(bytes.Length >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF);
        Assert.Equal("{\"name\":\"日本語\"}", File.ReadAllText(path));
    }

    /// Nothing may be left lying around next to the real file: a stray
    /// temporary would be indistinguishable from the store's own data to
    /// anything listing the directory.
    [Fact]
    public void LeavesNoTemporaryFileBehind()
    {
        var path = Path("clean.json");
        AtomicFile.WriteAllText(path, "one");
        AtomicFile.WriteAllText(path, "two");
        Assert.Equal(new[] { "clean.json" }, Directory.GetFiles(_directory).Select(System.IO.Path.GetFileName).Order());
    }

    /// A directory the store has never written to yet is created on the way,
    /// the same way AppPaths.DataDirectory does for the real one.
    [Fact]
    public void CreatesAMissingParentDirectory()
    {
        var path = Path(System.IO.Path.Combine("nested", "deeper", "file.json"));
        AtomicFile.WriteAllText(path, "made-it");
        Assert.Equal("made-it", File.ReadAllText(path));
    }
}
