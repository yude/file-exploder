using System.Text;

namespace FileExploder.Services;

/// Replaces a file's contents in one step, or not at all.
///
/// File.WriteAllText truncates the existing file and then writes into it, so
/// anything that interrupts it - a crash, a power loss, a disk that fills up
/// mid-write - leaves a half-written file where a complete one used to be.
/// Every store in this app keeps one JSON document that is worthless when
/// truncated, and they all read an unparseable file as "nothing saved":
///
/// - the saved-server list silently comes back empty, losing the user's
///   servers outright;
/// - the known-hosts store forgets every pinned fingerprint, dropping every
///   host back to trust-on-first-use and quietly taking the
///   man-in-the-middle protection that pinning exists for with it;
/// - preferences revert to defaults.
///
/// Writing a temporary file beside the target and renaming it over the top
/// makes the replacement atomic: a reader sees either the old contents or the
/// new ones, never a prefix of the new ones. This is the same discipline the
/// Go server's executor already applies to every copy it publishes.
internal static class AtomicFile
{
    public static void WriteAllText(string path, string contents)
    {
        var directory = Path.GetDirectoryName(path);
        if (!string.IsNullOrEmpty(directory))
        {
            Directory.CreateDirectory(directory);
        }

        // Beside the target rather than in the system temp directory: a
        // rename is only atomic within a single volume, and %APPDATA% and
        // %TEMP% are not guaranteed to be on the same one.
        var temporary = path + ".tmp-" + Guid.NewGuid().ToString("N");
        try
        {
            using (var stream = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            using (var writer = new StreamWriter(stream, new UTF8Encoding(encoderShouldEmitUTF8Identifier: false)))
            {
                writer.Write(contents);
                writer.Flush();
                // The rename below publishes whatever the file holds. Getting
                // the bytes onto the device first is what stops it publishing
                // a name that points at contents a power loss never wrote.
                stream.Flush(flushToDisk: true);
            }

            File.Move(temporary, path, overwrite: true);
        }
        catch
        {
            try
            {
                File.Delete(temporary);
            }
            catch (Exception)
            {
                // Best-effort: the original file is intact either way, which
                // is the point. A leftover temp file is not worth masking the
                // real failure being rethrown below.
            }
            throw;
        }
    }
}
