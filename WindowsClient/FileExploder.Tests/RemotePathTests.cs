using FileExploder.Utilities;

namespace FileExploder.Tests;

public class RemotePathTests
{
    [Fact]
    public void StandardizationIsLexicalAndRemote()
    {
        Assert.Equal("/srv/data/b", RemotePath.Standardized("/srv//data/./a/../b/"));
        Assert.Equal("/etc", RemotePath.Standardized("/../../etc"));
        Assert.Equal("relative/path", RemotePath.Standardized("relative/path"));
    }

    [Fact]
    public void DescendantRequiresAComponentBoundary()
    {
        Assert.True(RemotePath.IsDescendant("/srv/data", "/srv/data"));
        Assert.True(RemotePath.IsDescendant("/srv/data/child", "/srv/data"));
        Assert.False(RemotePath.IsDescendant("/srv/database", "/srv/data"));
        Assert.False(RemotePath.IsDescendant("relative", "/srv/data"));
    }

    [Fact]
    public void ParentStopsAtRoot()
    {
        Assert.Equal("/srv", RemotePath.Parent("/srv/data"));
        Assert.Equal("/", RemotePath.Parent("/"));
    }

    [Fact]
    public void AcceptsOrdinaryComponentNames()
    {
        foreach (var name in new[] { "file.txt", "フォルダ", "a b", "-dash", "..hidden", "a.b.c", "🙂" })
        {
            Assert.True(RemotePath.IsValidComponent(name), name);
        }
    }

    [Fact]
    public void RejectsAnythingThatIsNotOneComponent()
    {
        foreach (var name in new[] { "", ".", "..", "/", "a/b", "/leading", "trailing/", "nul\0" })
        {
            Assert.False(RemotePath.IsValidComponent(name), name);
        }
    }

    /// Unlike Swift, where a "/" immediately followed by a combining mark
    /// fuses into one indivisible Character (defeating a naive
    /// split(separator:) or Contains("/") check), .NET strings are UTF-16
    /// code-unit sequences with no such grapheme fusing: "/" and a following
    /// combining mark are always two distinct elements to split/search on.
    /// This documents that the hazard the Swift client had to special-case
    /// simply does not exist here.
    [Fact]
    public void ASeparatorBeforeACombiningMarkIsNeverHidden()
    {
        // U+0301 COMBINING ACUTE ACCENT, immediately after a "/".
        var hidden = "/a/́b";
        Assert.Equal("/a", RemotePath.Parent(hidden));
        Assert.True(RemotePath.IsDescendant("/srv/́archive/x", "/srv"));
        Assert.False(RemotePath.IsValidComponent("a/́b"));
    }

    [Fact]
    public void AcceptsAMoveIntoAnotherDirectory()
    {
        Assert.True(RemotePath.CanMove("/srv/a.txt", "/srv/archive"));
        Assert.True(RemotePath.CanMove("/srv/docs", "/srv/archive"));
        Assert.True(RemotePath.CanMove("/srv/docs/deep/file", "/srv"));
        Assert.True(RemotePath.CanMove("/srv/a.txt", "/"));
    }

    [Fact]
    public void RefusesAMoveIntoTheDirectoryItIsAlreadyIn()
    {
        Assert.False(RemotePath.CanMove("/srv/a.txt", "/srv"));
        Assert.False(RemotePath.CanMove("/srv/a.txt", "/srv/"));
        Assert.False(RemotePath.CanMove("/srv/a.txt", "/srv/./"));
        Assert.False(RemotePath.CanMove("/a.txt", "/"));
    }

    /// The drop the server rejects with "cannot move a directory into
    /// itself".
    [Fact]
    public void RefusesADirectoryDroppedIntoItsOwnSubtree()
    {
        Assert.False(RemotePath.CanMove("/srv/docs", "/srv/docs"));
        Assert.False(RemotePath.CanMove("/srv/docs", "/srv/docs/nested"));
        Assert.False(RemotePath.CanMove("/srv/docs", "/srv/docs/a/b/c"));
        // A sibling that merely shares a prefix is fine.
        Assert.True(RemotePath.CanMove("/srv/docs", "/srv/docs-backup"));
    }
}
