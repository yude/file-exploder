using System.Diagnostics;
using FileExploder.Models;
using FileExploder.Services;

namespace FileExploder.Tests;

/// Exercises SshConnection against a real, local sshd rather than only
/// unit-testing it in isolation - the timeout/output-limit/cancellation
/// behavior here depends on how SSH.NET actually behaves over a live
/// channel, which is exactly the kind of thing worth verifying for real.
[Collection("Local SSH")]
public sealed class SshConnectionTests(LocalSshFixture fixture)
{
    private SshConnection NewConnection(string? knownHostsFile = null) => new(
        new Server
        {
            Name = "test",
            Hostname = "localhost",
            Port = (ushort)fixture.Port,
            Username = fixture.Username,
            KeyPath = fixture.PrivateKeyPath,
        },
        new KnownHostsStore(knownHostsFile ?? Path.GetTempFileName()));

    [Fact]
    public async Task ConnectsAndRunsACommand()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();
        Assert.True(connection.IsConnected);

        var output = await connection.ExecuteCommandAsync("echo hello-from-test");
        Assert.Contains("hello-from-test", output);
    }

    [Fact]
    public async Task ReportsANonZeroExitAsACommandFailure()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();

        var ex = await Assert.ThrowsAsync<SshCommandFailedException>(
            () => connection.ExecuteCommandAsync("echo failure-detail 1>&2; exit 7"));
        Assert.Contains("failure-detail", ex.Message);
    }

    [Fact]
    public async Task TimesOutAHungCommandWithoutKillingTheConnection()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();

        var sw = Stopwatch.StartNew();
        await Assert.ThrowsAsync<SshCommandTimedOutException>(
            () => connection.ExecuteCommandAsync("sleep 5", timeout: TimeSpan.FromMilliseconds(300)));
        Assert.True(sw.ElapsedMilliseconds < 4000, $"took {sw.ElapsedMilliseconds}ms, should have given up around 300ms");

        // The connection itself must still be usable for the next command,
        // the same way the daemon's own per-job timeout leaves the rest of
        // the queue able to proceed.
        var output = await connection.ExecuteCommandAsync("echo still-alive");
        Assert.Contains("still-alive", output);
    }

    [Fact]
    public async Task StopsAtTheOutputLimitInsteadOfBufferingEverything()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();

        await Assert.ThrowsAsync<SshOutputTooLargeException>(() => connection.ExecuteCommandAsync(
            "yes line | head -c 200000",
            outputLimit: 1024));
    }

    [Fact]
    public async Task ProducesByteCorrectOutputForALargeResponse()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();

        const int lines = 2000;
        var output = await connection.ExecuteCommandAsync($"for i in $(seq 1 {lines}); do echo \"line-$i\"; done");
        var actual = output.Split('\n', StringSplitOptions.RemoveEmptyEntries);
        Assert.Equal(lines, actual.Length);
        for (var i = 0; i < lines; i++)
        {
            Assert.Equal($"line-{i + 1}", actual[i]);
        }
    }

    [Fact]
    public async Task TerminateAllStopsFurtherCommands()
    {
        var connection = NewConnection();
        await connection.TestConnectionAsync();

        connection.TerminateAll();

        await Assert.ThrowsAsync<OperationCanceledException>(() => connection.ExecuteCommandAsync("echo should-not-run"));
    }

    [Fact]
    public async Task RealConnectionRecordsItsHostKeyOnFirstUse()
    {
        var knownHostsFile = Path.GetTempFileName();
        var connection = NewConnection(knownHostsFile);

        await connection.TestConnectionAsync();

        // Recorded for real, against the fingerprint sshd actually
        // presented - not a value this test invented - so a second
        // connection reusing the file succeeds without prompting again.
        var second = NewConnection(knownHostsFile);
        await second.TestConnectionAsync();
        Assert.True(second.IsConnected);
    }
}
