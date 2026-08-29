using System.Text.Json;
using System.Text.Json.Serialization;
using FileExploder.Models;
using FileExploder.Utilities;

namespace FileExploder.Services;

/// Directory listing and queue operations, layered on top of a connected
/// SshConnection. Named after the macOS client's SFTPService for parity even
/// though - like that client - it never actually speaks the SFTP protocol:
/// every operation shells out to the `file-exploder` CLI on the remote host
/// and reads back the JSON it prints.
public sealed class SftpService(SshConnection ssh)
{
    private const string CommandPrefix = "PATH=\"$PATH:/usr/local/bin:$HOME/.local/bin\"; export PATH; file-exploder";

    private sealed class RemoteFileJson
    {
        [JsonPropertyName("name")]
        public required string Name { get; init; }

        [JsonPropertyName("path")]
        public required string Path { get; init; }

        [JsonPropertyName("size")]
        public long Size { get; init; }

        [JsonPropertyName("modificationDate")]
        public long ModificationDate { get; init; }

        [JsonPropertyName("isDirectory")]
        public bool IsDirectory { get; init; }

        /// Optional so the client keeps working against daemons predating
        /// the field.
        [JsonPropertyName("isSymlink")]
        public bool? IsSymlink { get; init; }

        [JsonPropertyName("permissions")]
        public uint Permissions { get; init; }
    }

    /// A single `list` invocation enumerates and lstats every entry of a
    /// directory in one unpaginated pass (see Server/cmd/list.go), so it can
    /// legitimately take much longer than an ordinary control command on a
    /// very large directory or a slow filesystem. The generic 120s default
    /// would time it out - permanently, since retrying hits the same
    /// directory-size-driven slowness every time - so give it its own, much
    /// larger budget instead of inheriting ExecuteCommandAsync's default.
    private static readonly TimeSpan ListDirectoryTimeout = TimeSpan.FromSeconds(900);

    /// A directory large enough to need ListDirectoryTimeout's longer budget
    /// is also large enough to produce a JSON response well past
    /// SshConnection's default 64MB stdout cap - which would otherwise fail
    /// exactly that case with SshOutputTooLargeException instead of
    /// succeeding within the longer timeout. 512MB comfortably covers
    /// directories far larger than the 900s timeout is itself sized for.
    private const int ListDirectoryOutputLimit = 512 * 1024 * 1024;

    public async Task<List<RemoteFile>> ListDirectoryAsync(string path, CancellationToken cancellationToken = default)
    {
        // Keep the path ASCII until it reaches the Go process. A composed
        // Linux filename passed straight through a shell command line risks
        // arriving byte-different than it left.
        var command = $"{CommandPrefix} list --path-base64 {path.Utf8Base64().ShellEscaped()}";
        var legacyCommand = $"{CommandPrefix} list -- {path.ShellEscaped()}";
        var output = await ExecuteWithLegacyFallbackAsync(
            command,
            legacyCommand,
            unsupportedFlags: ["--path-base64"],
            timeout: ListDirectoryTimeout,
            outputLimit: ListDirectoryOutputLimit,
            cancellationToken: cancellationToken).ConfigureAwait(false);

        var data = JsonPayload(output) ?? throw new SshInvalidResponseException("Empty response");
        var list = ParseJson<List<RemoteFileJson>>(data);
        return [.. list.Select(MakeRemoteFile)];
    }

    public async Task<string> AddToQueueAsync(string type, string? src, string? dst, string? mode = null, CancellationToken cancellationToken = default)
    {
        var command = $"{CommandPrefix} add --type {type}";
        var legacyCommand = command;
        if (src is not null)
        {
            command += $" --src-base64 {src.Utf8Base64().ShellEscaped()}";
            legacyCommand += $" --src {src.ShellEscaped()}";
        }
        if (dst is not null)
        {
            command += $" --dst-base64 {dst.Utf8Base64().ShellEscaped()}";
            legacyCommand += $" --dst {dst.ShellEscaped()}";
        }
        if (mode is not null)
        {
            command += $" --mode {mode.ShellEscaped()}";
            legacyCommand += $" --mode {mode.ShellEscaped()}";
        }

        var output = await ExecuteWithLegacyFallbackAsync(
            command,
            legacyCommand,
            unsupportedFlags: ["--src-base64", "--dst-base64"],
            cancellationToken: cancellationToken).ConfigureAwait(false);

        var data = JsonPayload(output);
        string? id = null;
        if (data is not null)
        {
            try
            {
                using var document = JsonDocument.Parse(data);
                if (document.RootElement.TryGetProperty("id", out var idElement) && idElement.ValueKind == JsonValueKind.String)
                {
                    id = idElement.GetString();
                }
            }
            catch (JsonException)
            {
                // Falls through to the invalidResponse below.
            }
        }
        if (id is null)
        {
            throw new SshInvalidResponseException($"不正なジョブ登録レスポンス: {output.TruncatedForDisplay()}");
        }
        return id;
    }

    public async Task<List<QueueJob>> GetQueueStatusAsync(CancellationToken cancellationToken = default)
    {
        var output = await ssh.ExecuteCommandAsync($"{CommandPrefix} status", cancellationToken: cancellationToken).ConfigureAwait(false);
        var data = JsonPayload(output) ?? throw new SshInvalidResponseException("Empty response");
        return ParseLenientJobsResponse(data);
    }

    /// Decoded element-by-element (see LenientJson) so one job with an
    /// operation type or status this client doesn't recognize - a newer
    /// server, or a build skew between client and server - doesn't hide every
    /// other job in the response.
    public async Task<List<QueueJob>> GetJobLogsAsync(int limit = 50, CancellationToken cancellationToken = default)
    {
        var output = await ssh.ExecuteCommandAsync($"{CommandPrefix} log --limit {limit}", cancellationToken: cancellationToken).ConfigureAwait(false);
        var data = JsonPayload(output) ?? throw new SshInvalidResponseException("Empty response");
        return ParseLenientArray<QueueJob>(data);
    }

    public async Task<QueueJob> GetJobStatusAsync(string id, CancellationToken cancellationToken = default)
    {
        var output = await ssh.ExecuteCommandAsync($"{CommandPrefix} status -- {id.ShellEscaped()}", cancellationToken: cancellationToken).ConfigureAwait(false);
        var data = JsonPayload(output) ?? throw new SshInvalidResponseException("Empty response");
        return ParseJson<QueueJob>(data);
    }

    /// How long a job may sit pending, with nothing else running, before the
    /// queue is treated as stalled.
    private static readonly TimeSpan StalledQueueGracePeriod = TimeSpan.FromSeconds(30);

    /// Consecutive polling failures tolerated before a wait gives up. Each
    /// poll is its own SSH command, so one refused connection or dropped
    /// session used to abort the wait and report a queued operation as
    /// failed - while the server went on to run it. Only an unbroken run of
    /// failures means the connection is really gone.
    private const int PollFailuresTolerated = 3;

    private static readonly TimeSpan InitialPollInterval = TimeSpan.FromMilliseconds(300);
    private static readonly TimeSpan MaxPollInterval = TimeSpan.FromSeconds(3);

    private const string StalledQueueMessage =
        "ジョブが開始されないままです。サーバーで file-exploder デーモンが動作しているか確認してください " +
        "(systemctl --user status file-exploder)。ジョブはキューに残っています。";

    /// Wait for a queued operation. Once a job is running the wait is
    /// unbounded: a large copy legitimately takes minutes and must not be
    /// reported as failed for missing an arbitrary UI deadline.
    ///
    /// A job that never even starts is a different story. If the daemon is
    /// not running - the case the README's `loginctl enable-linger` note is
    /// about - the job stays pending forever and every operation used to
    /// hang behind a spinner with nothing to act on. Give up only once the
    /// job has waited out the grace period *and* the server reports nothing
    /// running at all, twice in a row, so a busy queue is never mistaken for
    /// a dead one.
    public async Task WaitForJobAsync(string id, TimeSpan? timeout = null, CancellationToken cancellationToken = default)
    {
        var start = DateTimeOffset.UtcNow;
        var pollInterval = InitialPollInterval;
        var pendingSince = DateTimeOffset.UtcNow;
        var everStarted = false;
        var stalledObservations = 0;
        var consecutiveFailures = 0;

        while (timeout is null || DateTimeOffset.UtcNow - start < timeout.Value)
        {
            cancellationToken.ThrowIfCancellationRequested();

            QueueJob job;
            try
            {
                job = await GetJobStatusAsync(id, cancellationToken).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                throw;
            }
            catch (Exception)
            {
                consecutiveFailures++;
                if (consecutiveFailures > PollFailuresTolerated)
                {
                    throw;
                }
                await Task.Delay(pollInterval, cancellationToken).ConfigureAwait(false);
                pollInterval = Min(pollInterval * 2, MaxPollInterval);
                continue;
            }
            consecutiveFailures = 0;

            switch (job.Status)
            {
                case JobStatus.Completed:
                    return;
                case JobStatus.Failed:
                    throw new SshInvalidResponseException(job.Error ?? "Unknown error");
                case JobStatus.Cancelled:
                    throw new SshInvalidResponseException("ジョブがキャンセルされました");
                case JobStatus.Running:
                    everStarted = true;
                    break;
                case JobStatus.Pending:
                    if (!everStarted && DateTimeOffset.UtcNow - pendingSince > StalledQueueGracePeriod)
                    {
                        // A failure here says nothing about the queue, so
                        // treat it as "still moving" and let the next poll
                        // decide.
                        bool moving;
                        try
                        {
                            moving = await QueueIsMovingAsync(cancellationToken).ConfigureAwait(false);
                        }
                        catch (OperationCanceledException)
                        {
                            throw;
                        }
                        catch (Exception)
                        {
                            moving = true;
                        }

                        if (moving)
                        {
                            pendingSince = DateTimeOffset.UtcNow;
                            stalledObservations = 0;
                        }
                        else
                        {
                            stalledObservations++;
                            if (stalledObservations >= 2)
                            {
                                throw new SshInvalidResponseException(StalledQueueMessage);
                            }
                        }
                    }
                    break;
            }

            await Task.Delay(pollInterval, cancellationToken).ConfigureAwait(false);
            pollInterval = Min(pollInterval * 2, MaxPollInterval);
        }

        throw new SshInvalidResponseException("処理がタイムアウトしました。ジョブ画面で状態を確認してください");
    }

    /// Concurrency cap for resolving several jobs' final outcomes at once:
    /// high enough that a burst of simultaneous completions in a large batch
    /// doesn't serialize behind each other's SSH round trip, low enough that
    /// a very large batch doesn't spawn an unbounded number of concurrent
    /// commands in one instant.
    private const int MaxConcurrentJobLookups = 8;

    /// Waits for every job in `ids` to leave the active queue, sharing a
    /// single GetQueueStatusAsync() poll per interval across the whole batch
    /// instead of paying one GetJobStatusAsync() round trip per job per poll -
    /// which is what a bulk operation (multi-file delete/copy/move/chmod)
    /// used to do by calling WaitForJobAsync(id) once per file, fully
    /// serially.
    ///
    /// Once a job leaves the active list it is looked up individually
    /// exactly once, to learn how it actually finished - a bounded number of
    /// these lookups run concurrently, so a burst of simultaneous
    /// completions doesn't serialize behind each other's round trip either.
    /// Everything else about the wait - the stalled-queue detection, the
    /// poll-failure tolerance - mirrors WaitForJobAsync above, just shared
    /// across the batch instead of tracked per job. The one exception is
    /// deliberate: whether a job has ever been seen running is tracked per
    /// id (not one shared flag), so one job starting quickly doesn't disable
    /// stalled-queue detection for every other id in the same batch - a job
    /// that hasn't itself started waits out the same grace period
    /// WaitForJobAsync would give it alone.
    ///
    /// Returns the ids that did not complete successfully, mapped to an
    /// error message; an id that isn't a key in the result succeeded.
    public async Task<Dictionary<string, string>> WaitForJobsAsync(IReadOnlyList<string> ids, CancellationToken cancellationToken = default)
    {
        if (ids.Count == 0)
        {
            return [];
        }

        var remaining = new HashSet<string>(ids);
        var failures = new Dictionary<string, string>();
        var startedIds = new HashSet<string>();

        var pollInterval = InitialPollInterval;
        var pendingSince = DateTimeOffset.UtcNow;
        var stalledObservations = 0;
        var consecutiveFailures = 0;

        while (remaining.Count > 0)
        {
            cancellationToken.ThrowIfCancellationRequested();

            List<QueueJob> activeJobs;
            try
            {
                activeJobs = await GetQueueStatusAsync(cancellationToken).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                throw;
            }
            catch (Exception)
            {
                consecutiveFailures++;
                if (consecutiveFailures > PollFailuresTolerated)
                {
                    throw;
                }
                await Task.Delay(pollInterval, cancellationToken).ConfigureAwait(false);
                pollInterval = Min(pollInterval * 2, MaxPollInterval);
                continue;
            }
            consecutiveFailures = 0;

            var activeById = activeJobs.ToDictionary(job => job.Id, job => job);
            var queueIsMoving = activeJobs.Any(job => job.Status == JobStatus.Running);
            foreach (var id in remaining)
            {
                if (activeById.TryGetValue(id, out var active) && active.Status == JobStatus.Running)
                {
                    startedIds.Add(id);
                }
            }

            var justFinished = remaining.Where(id => !activeById.ContainsKey(id)).ToList();
            foreach (var (id, result, error) in await ResolveOutcomesAsync(justFinished, GetJobStatusAsync, cancellationToken).ConfigureAwait(false))
            {
                remaining.Remove(id);
                if (error is not null)
                {
                    failures[id] = error.Message;
                    continue;
                }

                var job = result!;
                switch (job.Status)
                {
                    case JobStatus.Completed:
                        break;
                    case JobStatus.Failed:
                        failures[id] = job.Error ?? "Unknown error";
                        break;
                    case JobStatus.Cancelled:
                        failures[id] = "ジョブがキャンセルされました";
                        break;
                    case JobStatus.Pending:
                    case JobStatus.Running:
                        // Reappeared between the batch poll and this lookup -
                        // still moving; pick it back up next round.
                        remaining.Add(id);
                        break;
                }
            }
            if (remaining.Count == 0)
            {
                break;
            }

            // Only ids that have never themselves started are eligible for
            // the stalled-queue failure below; an id that already started
            // keeps waiting unbounded, exactly like WaitForJobAsync does for
            // a single job.
            var neverStarted = remaining.Where(id => !startedIds.Contains(id)).ToList();
            if (neverStarted.Count > 0 && DateTimeOffset.UtcNow - pendingSince > StalledQueueGracePeriod)
            {
                if (queueIsMoving)
                {
                    pendingSince = DateTimeOffset.UtcNow;
                    stalledObservations = 0;
                }
                else
                {
                    stalledObservations++;
                    if (stalledObservations >= 2)
                    {
                        foreach (var id in neverStarted)
                        {
                            failures[id] = StalledQueueMessage;
                            remaining.Remove(id);
                        }
                        if (remaining.Count == 0)
                        {
                            break;
                        }
                    }
                }
            }

            await Task.Delay(pollInterval, cancellationToken).ConfigureAwait(false);
            pollInterval = Min(pollInterval * 2, MaxPollInterval);
        }

        return failures;
    }

    /// Resolves each id in `ids` with `lookup`, running up to
    /// MaxConcurrentJobLookups at once instead of one at a time, so a burst
    /// of simultaneously-finished jobs doesn't serialize behind each other's
    /// round trip.
    private static async Task<List<(string Id, QueueJob? Job, Exception? Error)>> ResolveOutcomesAsync(
        IReadOnlyList<string> ids,
        Func<string, CancellationToken, Task<QueueJob>> lookup,
        CancellationToken cancellationToken)
    {
        var results = new List<(string, QueueJob?, Exception?)>(ids.Count);
        var nextIndex = 0;
        var gate = new Lock();

        async Task WorkerAsync()
        {
            while (true)
            {
                string id;
                lock (gate)
                {
                    if (nextIndex >= ids.Count)
                    {
                        return;
                    }
                    id = ids[nextIndex];
                    nextIndex++;
                }

                (string, QueueJob?, Exception?) outcome;
                try
                {
                    var job = await lookup(id, cancellationToken).ConfigureAwait(false);
                    outcome = (id, job, null);
                }
                catch (OperationCanceledException)
                {
                    throw;
                }
                catch (Exception ex)
                {
                    outcome = (id, null, ex);
                }

                lock (gate)
                {
                    results.Add(outcome);
                }
            }
        }

        var workerCount = Math.Min(MaxConcurrentJobLookups, ids.Count);
        var workers = Enumerable.Range(0, workerCount).Select(_ => WorkerAsync());
        await Task.WhenAll(workers).ConfigureAwait(false);
        return results;
    }

    private async Task<bool> QueueIsMovingAsync(CancellationToken cancellationToken) =>
        (await GetQueueStatusAsync(cancellationToken).ConfigureAwait(false)).Any(job => job.Status == JobStatus.Running);

    public async Task CancelJobAsync(string id, CancellationToken cancellationToken = default)
    {
        await ssh.ExecuteCommandAsync($"{CommandPrefix} cancel -- {id.ShellEscaped()}", cancellationToken: cancellationToken).ConfigureAwait(false);
    }

    private static TimeSpan Min(TimeSpan a, TimeSpan b) => a < b ? a : b;

    // MARK: - Private helpers

    /// Keep a new client usable with an older server. Cobra rejects unknown
    /// flags before running the command, so retrying only this exact error
    /// can never enqueue an operation twice.
    private async Task<string> ExecuteWithLegacyFallbackAsync(
        string command,
        string legacyCommand,
        string[] unsupportedFlags,
        TimeSpan? timeout = null,
        int outputLimit = SshConnection.DefaultOutputLimit,
        CancellationToken cancellationToken = default)
    {
        try
        {
            return await ssh.ExecuteCommandAsync(command, timeout, outputLimit, cancellationToken).ConfigureAwait(false);
        }
        catch (SshCommandFailedException ex)
        {
            var isUnsupported = unsupportedFlags.Any(flag => ex.Message.Contains($"unknown flag: {flag}", StringComparison.Ordinal));
            if (!isUnsupported)
            {
                throw;
            }
            return await ssh.ExecuteCommandAsync(legacyCommand, timeout, outputLimit, cancellationToken).ConfigureAwait(false);
        }
    }

    private static RemoteFile MakeRemoteFile(RemoteFileJson item) => new(
        name: item.Name,
        path: item.Path,
        size: item.Size,
        modificationDate: DateTimeOffset.FromUnixTimeSeconds(item.ModificationDate),
        isDirectory: item.IsDirectory,
        isSymlink: item.IsSymlink ?? false,
        permissions: FilePermissions.FromOctal((int)item.Permissions));

    /// The JSON a server command produced, picked out of whatever else came
    /// back on stdout.
    ///
    /// Every command writes exactly one JSON document on exactly one line -
    /// encoding/json's Encoder emits compact output followed by a newline -
    /// but it shares stdout with the login shell that ran it. A banner, a
    /// fortune, an `echo` in a profile above the non-interactive guard: any
    /// of it arrives first and used to make every single command fail to
    /// parse, with nothing in the app working until the server's shell
    /// startup was tracked down. A literal newline cannot appear inside a
    /// JSON string, so the last non-empty line is the document.
    private static string? JsonPayload(string output)
    {
        var lastLine = output
            .Split('\n')
            .Select(line => line.Trim())
            .LastOrDefault(line => line.Length > 0);
        return lastLine ?? (output.Length > 0 ? output : null);
    }

    private static T ParseJson<T>(string json)
    {
        try
        {
            return JsonSerializer.Deserialize<T>(json) ?? throw new SshInvalidResponseException("empty result");
        }
        catch (JsonException ex)
        {
            var raw = json.Trim();
            if (raw.Length > 0)
            {
                throw new SshInvalidResponseException($"レスポンスの解析に失敗しました: {ex.Message}\nサーバー応答: {raw.TruncatedForDisplay()}");
            }
            throw new SshInvalidResponseException($"レスポンスの解析に失敗しました: {ex.Message}");
        }
    }

    private static List<T> ParseLenientArray<T>(string json)
    {
        try
        {
            return LenientJson.DeserializeLenientArray<T>(json, JsonSerializerOptions.Default);
        }
        catch (JsonException ex)
        {
            throw new SshInvalidResponseException($"レスポンスの解析に失敗しました: {ex.Message}");
        }
    }

    private static List<QueueJob> ParseLenientJobsResponse(string json)
    {
        try
        {
            using var document = JsonDocument.Parse(json);
            if (!document.RootElement.TryGetProperty("jobs", out var jobsElement) || jobsElement.ValueKind != JsonValueKind.Array)
            {
                return [];
            }
            return LenientJson.DeserializeLenientArray<QueueJob>(jobsElement, JsonSerializerOptions.Default);
        }
        catch (JsonException ex)
        {
            throw new SshInvalidResponseException($"レスポンスの解析に失敗しました: {ex.Message}");
        }
    }
}

file static class StringTruncationExtensions
{
    public static string TruncatedForDisplay(this string value)
    {
        const int limit = 4096;
        if (value.Length <= limit)
        {
            return value;
        }
        return value[..limit] + "\n…(truncated)";
    }
}
