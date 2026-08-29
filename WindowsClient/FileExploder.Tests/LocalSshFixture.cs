using System.Diagnostics;

namespace FileExploder.Tests;

/// Provisions a throwaway keypair authorized against the *current* user's own
/// account and hands tests a Server pointing at `localhost` over the local
/// sshd - so SshConnection can be exercised against a real SSH server
/// instead of only unit-tested in isolation. Requires `ssh-keygen` and a
/// running, reachable `sshd` on port 22 for the user running the tests; skip
/// or exclude this collection in an environment without one.
public sealed class LocalSshFixture : IDisposable
{
    private const string Marker = "file-exploder-tests-ephemeral-key";

    public string PrivateKeyPath { get; }
    public string Username { get; } = Environment.UserName;
    public int Port { get; } = 22;

    private readonly string _publicKeyLine;
    private readonly string _authorizedKeysPath;
    private readonly bool _appendedLine;

    public LocalSshFixture()
    {
        var directory = Directory.CreateTempSubdirectory("file-exploder-ssh-tests");
        PrivateKeyPath = Path.Combine(directory.FullName, "id_ed25519");

        RunSshKeygen(PrivateKeyPath);

        var publicKeyPath = PrivateKeyPath + ".pub";
        var publicKey = File.ReadAllText(publicKeyPath).TrimEnd('\n');
        _publicKeyLine = $"{publicKey} {Marker}";

        var sshDirectory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ssh");
        Directory.CreateDirectory(sshDirectory);
        _authorizedKeysPath = Path.Combine(sshDirectory, "authorized_keys");

        var existing = File.Exists(_authorizedKeysPath) ? File.ReadAllText(_authorizedKeysPath) : "";
        if (!existing.Contains(_publicKeyLine, StringComparison.Ordinal))
        {
            File.AppendAllText(_authorizedKeysPath, _publicKeyLine + "\n");
            _appendedLine = true;
        }
    }

    private static void RunSshKeygen(string privateKeyPath)
    {
        using var process = Process.Start(new ProcessStartInfo("ssh-keygen")
        {
            ArgumentList = { "-t", "ed25519", "-f", privateKeyPath, "-N", "", "-C", "file-exploder-tests" },
            RedirectStandardOutput = true,
            RedirectStandardError = true,
        }) ?? throw new InvalidOperationException("failed to start ssh-keygen");
        process.WaitForExit();
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException($"ssh-keygen exited with {process.ExitCode}: {process.StandardError.ReadToEnd()}");
        }
    }

    public void Dispose()
    {
        if (_appendedLine && File.Exists(_authorizedKeysPath))
        {
            var lines = File.ReadAllLines(_authorizedKeysPath).Where(line => line != _publicKeyLine);
            File.WriteAllLines(_authorizedKeysPath, lines);
        }
    }
}

[CollectionDefinition("Local SSH")]
public sealed class LocalSshCollection : ICollectionFixture<LocalSshFixture>;
