namespace FileExploder.Services;

/// Where this app keeps its own state - the saved-server list and the
/// known-hosts store. On Windows this resolves under %APPDATA%; the same
/// special folder exists (as $XDG_CONFIG_HOME-or-~/.config equivalent) on
/// Linux/macOS too, which is what this runs against in development.
public static class AppPaths
{
    public static string DataDirectory
    {
        get
        {
            var root = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData);
            var path = Path.Combine(root, "FileExploder");
            Directory.CreateDirectory(path);
            return path;
        }
    }

    public static string SavedServersFile => Path.Combine(DataDirectory, "servers.json");

    public static string KnownHostsFile => Path.Combine(DataDirectory, "known_hosts.json");
}
