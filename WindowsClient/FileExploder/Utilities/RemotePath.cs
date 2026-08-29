namespace FileExploder.Utilities;

/// Lexical path handling for paths that live on the *remote* host.
///
/// Paths are normalised purely lexically here - the same thing
/// filepath.Clean does on the server side - rather than resolved against
/// this machine's own filesystem, which (running on Windows) wouldn't even
/// use the same path syntax as the remote Linux host.
///
/// Every comparison and search here is explicit about
/// <see cref="StringComparison.Ordinal"/>. Unlike `==`, which is already
/// ordinal by default for .NET strings, StartsWith/Contains/IndexOf default
/// to the *current culture* unless told otherwise - which can treat
/// different byte sequences as equivalent (or vice versa) depending on the
/// user's locale. A path is a byte sequence the server compares
/// byte-for-byte, so nothing here may depend on locale.
public static class RemotePath
{
    /// Collapses ".", "..", duplicate separators and any trailing separator.
    /// Relative input is returned untouched: callers reject it separately,
    /// and silently rooting it would invent a path the user never asked for.
    public static string Standardized(string path)
    {
        if (path.Length == 0 || path[0] != '/')
        {
            return path;
        }

        var components = new List<string>();
        foreach (var component in SplitComponents(path))
        {
            switch (component)
            {
                case ".":
                    continue;
                case "..":
                    if (components.Count > 0)
                    {
                        components.RemoveAt(components.Count - 1);
                    }
                    break;
                default:
                    components.Add(component);
                    break;
            }
        }
        return "/" + string.Join('/', components);
    }

    public static string Parent(string path)
    {
        var components = SplitComponents(Standardized(path));
        if (components.Count == 0)
        {
            return "/";
        }
        components.RemoveAt(components.Count - 1);
        return "/" + string.Join('/', components);
    }

    public static string Appending(string name, string path)
    {
        var basePath = Standardized(path);
        return basePath == "/" ? "/" + name : basePath + "/" + name;
    }

    /// Whether `name` can stand as a single path component.
    public static bool IsValidComponent(string name)
    {
        if (name.Length == 0 || name == "." || name == "..")
        {
            return false;
        }
        return name.IndexOf('/') < 0 && name.IndexOf('\0') < 0;
    }

    /// Whether `path` could be moved into `destination`.
    ///
    /// Rejects the two drops the server would only fail on: the entry is
    /// already in that directory, and a directory dropped onto itself or
    /// into its own subtree. Refusing them here means the drag is declined
    /// while the user can still see what they aimed at, instead of a job
    /// appearing in the queue and failing a moment later.
    public static bool CanMove(string path, string destination)
    {
        var target = Standardized(destination);
        if (Parent(path) == target)
        {
            return false;
        }
        return !IsDescendant(target, path);
    }

    /// Whether `path` is `root` or sits underneath it. Both sides are
    /// standardized first so "/srv/data/../data/x" is recognised as inside
    /// "/srv/data".
    public static bool IsDescendant(string path, string root)
    {
        var standardizedRoot = Standardized(root);
        var target = Standardized(path);
        if (!StartsWithSlash(standardizedRoot) || !StartsWithSlash(target))
        {
            return false;
        }
        if (standardizedRoot == "/")
        {
            return true;
        }
        return target == standardizedRoot
            || target.StartsWith(standardizedRoot + "/", StringComparison.Ordinal);
    }

    private static bool StartsWithSlash(string value) => value.Length > 0 && value[0] == '/';

    /// Splits a path into its non-empty components - leading, trailing and
    /// duplicated separators are all omitted, matching Go's filepath.Clean
    /// semantics for the same input.
    public static List<string> SplitComponents(string path) =>
        path.Split('/', StringSplitOptions.RemoveEmptyEntries).ToList();
}
