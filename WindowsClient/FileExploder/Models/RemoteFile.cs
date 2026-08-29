using FileExploder.Utilities;

namespace FileExploder.Models;

/// One entry from a directory listing.
///
/// Identity (see <see cref="Id"/>) is deliberately narrower than structural
/// equality: two listings of the same path are "the same row" for
/// selection/drag-and-drop purposes even if, say, its size changed between
/// an auto-refresh tick and the one before it.
public sealed class RemoteFile : IEquatable<RemoteFile>
{
    public string Name { get; }
    public string Path { get; }
    public long Size { get; }
    public DateTimeOffset ModificationDate { get; }
    public bool IsDirectory { get; }
    public bool IsSymlink { get; }
    public FilePermissions Permissions { get; }

    public RemoteFile(
        string name,
        string path,
        long size,
        DateTimeOffset modificationDate,
        bool isDirectory,
        bool isSymlink,
        FilePermissions permissions)
    {
        Name = name;
        Path = path;
        Size = size;
        ModificationDate = modificationDate;
        IsDirectory = isDirectory;
        IsSymlink = isSymlink;
        Permissions = permissions;
    }

    /// Row identity for selection, drag-and-drop and lookups - this is
    /// `Path` itself, not a re-encoded form of it.
    ///
    /// The Swift client hex-encodes this path's UTF-8 bytes instead, because
    /// Swift's default String equality treats canonically-equivalent Unicode
    /// spellings (an NFC and an NFD form of the same visible name) as equal,
    /// while a Linux server keeps them as two genuinely different files - a
    /// directory shared with a Mac routinely ends up holding both, and every
    /// identity check needs to keep them apart. .NET's default string
    /// equality is ordinal (byte-for-byte on the UTF-16 code units, no
    /// Unicode normalization), so an NFC and an NFD spelling already compare
    /// as different strings here - the platform gives this guarantee for
    /// free, without needing the same workaround.
    public string Id => Path;

    public string FormattedSize => IsDirectory ? "--" : FormatUtils.FormattedSize(Size);
    public string FormattedDate => FormatUtils.FormattedDate(ModificationDate);
    public string SymbolicPermissions => Permissions.SymbolicString(IsDirectory, IsSymlink);

    public bool Equals(RemoteFile? other) => other is not null && string.Equals(Id, other.Id, StringComparison.Ordinal);
    public override bool Equals(object? obj) => Equals(obj as RemoteFile);
    public override int GetHashCode() => StringComparer.Ordinal.GetHashCode(Id);
}

public sealed record FilePermissions(
    bool OwnerRead,
    bool OwnerWrite,
    bool OwnerExecute,
    bool GroupRead,
    bool GroupWrite,
    bool GroupExecute,
    bool OtherRead,
    bool OtherWrite,
    bool OtherExecute)
{
    public string SymbolicString(bool isDirectory, bool isSymlink)
    {
        var kind = isSymlink ? 'l' : isDirectory ? 'd' : '-';
        Span<char> chars = stackalloc char[10];
        chars[0] = kind;
        chars[1] = OwnerRead ? 'r' : '-';
        chars[2] = OwnerWrite ? 'w' : '-';
        chars[3] = OwnerExecute ? 'x' : '-';
        chars[4] = GroupRead ? 'r' : '-';
        chars[5] = GroupWrite ? 'w' : '-';
        chars[6] = GroupExecute ? 'x' : '-';
        chars[7] = OtherRead ? 'r' : '-';
        chars[8] = OtherWrite ? 'w' : '-';
        chars[9] = OtherExecute ? 'x' : '-';
        return new string(chars);
    }

    public static FilePermissions FromOctal(int octal) => new(
        OwnerRead: (octal & 0b100_000_000) != 0,
        OwnerWrite: (octal & 0b010_000_000) != 0,
        OwnerExecute: (octal & 0b001_000_000) != 0,
        GroupRead: (octal & 0b000_100_000) != 0,
        GroupWrite: (octal & 0b000_010_000) != 0,
        GroupExecute: (octal & 0b000_001_000) != 0,
        OtherRead: (octal & 0b000_000_100) != 0,
        OtherWrite: (octal & 0b000_000_010) != 0,
        OtherExecute: (octal & 0b000_000_001) != 0);
}
