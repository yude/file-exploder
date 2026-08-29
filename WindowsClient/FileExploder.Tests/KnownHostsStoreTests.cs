using FileExploder.Services;

namespace FileExploder.Tests;

public class KnownHostsStoreTests
{
    private static string NewTempFile()
    {
        var path = Path.GetTempFileName();
        File.Delete(path); // the store should tolerate a file that doesn't exist yet
        return path;
    }

    [Fact]
    public void TrustsAndRecordsAHostSeenForTheFirstTime()
    {
        var store = new KnownHostsStore(NewTempFile());
        Assert.True(store.TrustAndRecord("example.test", 22, "sha256:abc"));
    }

    [Fact]
    public void TrustsTheSameFingerprintOnLaterConnections()
    {
        var store = new KnownHostsStore(NewTempFile());
        Assert.True(store.TrustAndRecord("example.test", 22, "sha256:abc"));
        Assert.True(store.TrustAndRecord("example.test", 22, "sha256:abc"));
    }

    /// The whole point: a host whose key changed after being trusted once
    /// must not be silently trusted again.
    [Fact]
    public void RejectsAChangedFingerprintForAnAlreadyKnownHost()
    {
        var store = new KnownHostsStore(NewTempFile());
        Assert.True(store.TrustAndRecord("example.test", 22, "sha256:abc"));
        Assert.False(store.TrustAndRecord("example.test", 22, "sha256:different"));
    }

    [Fact]
    public void DistinguishesHostsByPortToo()
    {
        var store = new KnownHostsStore(NewTempFile());
        Assert.True(store.TrustAndRecord("example.test", 22, "sha256:abc"));
        Assert.True(store.TrustAndRecord("example.test", 2222, "sha256:different"));
    }

    [Fact]
    public void PersistsAcrossStoreInstances()
    {
        var path = NewTempFile();
        var first = new KnownHostsStore(path);
        Assert.True(first.TrustAndRecord("example.test", 22, "sha256:abc"));

        var second = new KnownHostsStore(path);
        Assert.True(second.TrustAndRecord("example.test", 22, "sha256:abc"));
        Assert.False(second.TrustAndRecord("example.test", 22, "sha256:different"));
    }
}
