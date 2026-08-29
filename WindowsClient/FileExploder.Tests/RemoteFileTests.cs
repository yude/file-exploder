using FileExploder.Models;

namespace FileExploder.Tests;

public class RemoteFileIdentityTests
{
    private static RemoteFile File(string name, string directory = "/srv") => new(
        name: name,
        path: directory + "/" + name,
        size: 0,
        modificationDate: DateTimeOffset.UnixEpoch,
        isDirectory: false,
        isSymlink: false,
        permissions: FilePermissions.FromOctal(0b110_100_100));

    /// A Linux server holds an NFC and an NFD spelling of the same visible
    /// name as two separate files. Swift needs a byte-encoding workaround to
    /// keep them apart (its default String equality is Unicode-canonical);
    /// .NET's default string equality is already ordinal, so this is really
    /// confirming the platform gives that guarantee for free.
    [Fact]
    public void CanonicallyEquivalentNamesGetDistinctIdentities()
    {
        var composed = File("café.txt"); // NFC
        var decomposed = File("café.txt"); // NFD

        Assert.Equal(composed.Path, composed.Path); // sanity
        Assert.NotEqual(composed.Path, decomposed.Path, StringComparer.Ordinal);
        Assert.NotEqual(composed.Id, decomposed.Id, StringComparer.Ordinal);
        Assert.NotEqual(composed, decomposed);
        Assert.Equal(2, new HashSet<RemoteFile> { composed, decomposed }.Count);
        Assert.Equal(2, new HashSet<string>(StringComparer.Ordinal) { composed.Id, decomposed.Id }.Count);
    }

    [Fact]
    public void IdentityIsStableAndDistinguishesDifferentPaths()
    {
        Assert.Equal(File("a.txt").Id, File("a.txt").Id, StringComparer.Ordinal);
        Assert.NotEqual(File("a.txt").Id, File("b.txt").Id, StringComparer.Ordinal);
        Assert.NotEqual(File("a.txt", "/srv").Id, File("a.txt", "/srv2").Id, StringComparer.Ordinal);
    }
}

public class FilePermissionsTests
{
    [Fact]
    public void SymbolicStringMatchesUnixConventions()
    {
        var rwxrxrx = FilePermissions.FromOctal(0b111_101_101);
        Assert.Equal("-rwxr-xr-x", rwxrxrx.SymbolicString(isDirectory: false, isSymlink: false));
        Assert.Equal("drwxr-xr-x", rwxrxrx.SymbolicString(isDirectory: true, isSymlink: false));
        Assert.Equal("lrwxr-xr-x", rwxrxrx.SymbolicString(isDirectory: false, isSymlink: true));

        var rw = FilePermissions.FromOctal(0b110_100_100);
        Assert.Equal("-rw-r--r--", rw.SymbolicString(isDirectory: false, isSymlink: false));
    }

    [Fact]
    public void FromOctalRoundTripsThroughSymbolicString()
    {
        var permissions = FilePermissions.FromOctal(0b111_000_000); // 700
        Assert.Equal("-rwx------", permissions.SymbolicString(isDirectory: false, isSymlink: false));
    }
}
