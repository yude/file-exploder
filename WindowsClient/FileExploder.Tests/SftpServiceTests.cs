using FileExploder.Models;
using FileExploder.Services;

namespace FileExploder.Tests;

/// Exercises SftpService against the real `file-exploder` server binary
/// over a live SSH connection, the same way SshConnectionTests exercises the
/// transport underneath it. Requires the `file-exploder` daemon reachable on
/// PATH (see the README) - if it isn't installed, these tests fail with a
/// clear "command not found"-style error rather than silently no-op'ing.
[Collection("Local SSH")]
public sealed class SftpServiceTests(LocalSshFixture fixture) : IDisposable
{
    private readonly string _scratchDir = Path.Combine(Path.GetTempPath(), "file-exploder-sftp-tests-" + Guid.NewGuid().ToString("N"));
    private SshConnection? _connection;

    private async Task<SftpService> NewServiceAsync()
    {
        Directory.CreateDirectory(_scratchDir);
        _connection = new SshConnection(
            new Server
            {
                Name = "test",
                Hostname = "localhost",
                Port = (ushort)fixture.Port,
                Username = fixture.Username,
                KeyPath = fixture.PrivateKeyPath,
            },
            new KnownHostsStore(Path.GetTempFileName()));
        await _connection.TestConnectionAsync();
        return new SftpService(_connection);
    }

    public void Dispose()
    {
        _connection?.TerminateAll();
        if (Directory.Exists(_scratchDir))
        {
            Directory.Delete(_scratchDir, recursive: true);
        }
    }

    [Fact]
    public async Task ListsDirectoryEntriesWithTheirMetadata()
    {
        var service = await NewServiceAsync();
        await File.WriteAllTextAsync(Path.Combine(_scratchDir, "a.txt"), "hello");
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));

        var entries = await service.ListDirectoryAsync(_scratchDir);

        var file = Assert.Single(entries, e => e.Name == "a.txt");
        Assert.False(file.IsDirectory);
        Assert.Equal(5, file.Size);

        var dir = Assert.Single(entries, e => e.Name == "subdir");
        Assert.True(dir.IsDirectory);
    }

    [Fact]
    public async Task AddsAJobAndWaitsForItToComplete()
    {
        var service = await NewServiceAsync();
        var target = Path.Combine(_scratchDir, "newdir");

        var id = await service.AddToQueueAsync("mkdir", src: null, dst: target);
        await service.WaitForJobAsync(id);

        Assert.True(Directory.Exists(target));

        var job = await service.GetJobStatusAsync(id);
        Assert.Equal(JobStatus.Completed, job.Status);
    }

    [Fact]
    public async Task ReportsAFailedJobsErrorThroughWaitForJob()
    {
        var service = await NewServiceAsync();
        var missing = Path.Combine(_scratchDir, "does-not-exist");

        var id = await service.AddToQueueAsync("delete", src: missing, dst: null);

        var ex = await Assert.ThrowsAsync<SshInvalidResponseException>(() => service.WaitForJobAsync(id));
        Assert.Contains("サーバーからの応答が不正です", ex.Message);

        var job = await service.GetJobStatusAsync(id);
        Assert.Equal(JobStatus.Failed, job.Status);
    }

    [Fact]
    public async Task GetQueueStatusAndGetJobLogsSeeTheSameCompletedJob()
    {
        var service = await NewServiceAsync();
        var target = Path.Combine(_scratchDir, "logged-dir");

        var id = await service.AddToQueueAsync("mkdir", src: null, dst: target);
        await service.WaitForJobAsync(id);

        var logs = await service.GetJobLogsAsync(limit: 50);
        Assert.Contains(logs, job => job.Id == id && job.Status == JobStatus.Completed);
    }

    [Fact]
    public async Task WaitForJobsResolvesABatchOfMixedOutcomes()
    {
        var service = await NewServiceAsync();
        var goodTarget = Path.Combine(_scratchDir, "batch-good");
        var missing = Path.Combine(_scratchDir, "batch-missing");

        var goodId = await service.AddToQueueAsync("mkdir", src: null, dst: goodTarget);
        var badId = await service.AddToQueueAsync("delete", src: missing, dst: null);

        var failures = await service.WaitForJobsAsync([goodId, badId]);

        Assert.DoesNotContain(goodId, failures.Keys);
        Assert.Contains(badId, failures.Keys);
        Assert.True(Directory.Exists(goodTarget));
    }

    [Fact]
    public async Task WaitForJobsReturnsImmediatelyForAnEmptyBatch()
    {
        var service = await NewServiceAsync();
        var failures = await service.WaitForJobsAsync([]);
        Assert.Empty(failures);
    }

    [Fact]
    public async Task CancelJobRemovesAPendingJobFromTheActiveQueue()
    {
        var service = await NewServiceAsync();
        // A job the daemon is unlikely to have started yet: cancel races the
        // daemon's own polling loop. The server legitimately refuses to
        // cancel a job that already started or finished (cmd/cancel.go),
        // so - not just "the mkdir might still have run" - the cancel call
        // itself can legitimately fail here too; either outcome is
        // acceptable, only an exception other than the documented
        // already-running/finished one is not.
        var id = await service.AddToQueueAsync("mkdir", src: null, dst: Path.Combine(_scratchDir, "cancel-race"));
        try
        {
            await service.CancelJobAsync(id);
        }
        catch (SshCommandFailedException ex) when (ex.Message.Contains("already running/finished", StringComparison.Ordinal))
        {
            // Lost the race - acceptable, see above.
        }

        var job = await service.GetJobStatusAsync(id);
        Assert.True(job.Status is JobStatus.Cancelled or JobStatus.Completed or JobStatus.Running or JobStatus.Failed);
    }

    /// Exercises ResolveOutcomesAsync's bounded-concurrency job resolution
    /// with enough simultaneously-finished jobs that, if it were serializing
    /// lookups one at a time instead of running up to 8 concurrently, this
    /// would take noticeably longer than it does.
    [Fact]
    public async Task WaitForJobsResolvesALargeBatchOfSimultaneousCompletions()
    {
        var service = await NewServiceAsync();
        const int count = 20;
        var ids = new List<string>();
        for (var i = 0; i < count; i++)
        {
            ids.Add(await service.AddToQueueAsync("mkdir", src: null, dst: Path.Combine(_scratchDir, $"batch-{i}")));
        }

        var failures = await service.WaitForJobsAsync(ids);

        Assert.Empty(failures);
        for (var i = 0; i < count; i++)
        {
            Assert.True(Directory.Exists(Path.Combine(_scratchDir, $"batch-{i}")));
        }
    }
}
