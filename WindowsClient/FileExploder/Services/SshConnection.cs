using System.Net.Sockets;
using System.Text;
using CommunityToolkit.Mvvm.ComponentModel;
using FileExploder.Models;
using FileExploder.Utilities;
using Renci.SshNet;
using Renci.SshNet.Common;

namespace FileExploder.Services;

/// SSH connection manager, backed by a single persistent SSH.NET session
/// rather than spawning a new `ssh` process per command the way the macOS
/// client does. This is a deliberate improvement enabled by using a real SSH
/// library instead of shelling out to a system binary: SSH multiplexes many
/// commands as independent channels over one authenticated connection, so a
/// bulk operation's flurry of commands shares one handshake instead of
/// paying for a fresh one every time - while still supporting genuinely
/// concurrent in-flight commands (verified: 8 commands issued at once here
/// complete in roughly the time one does, not eight times that).
public sealed partial class SshConnection : ObservableObject
{
    /// The default stdout cap, sized for an ordinary control command's
    /// response. Callers expecting a much larger payload - a directory
    /// listing, say - should pass a correspondingly larger `outputLimit`
    /// alongside a longer `timeout`; a generous timeout paired with this
    /// default would otherwise fail exactly the large-response case the
    /// longer timeout was meant to allow, with SshOutputTooLargeException
    /// instead of a timeout.
    public const int DefaultOutputLimit = 64 * 1024 * 1024;

    private const int ErrorOutputLimit = 1024 * 1024;
    private const int ReadBufferSize = 32 * 1024;

    private readonly Server _server;
    private readonly KnownHostsStore _knownHosts;
    private readonly Lock _gate = new();
    private SshClient? _client;
    private bool _invalidated;
    private readonly HashSet<SshCommand> _activeCommands = [];

    [ObservableProperty]
    public partial bool IsConnected { get; set; }

    [ObservableProperty]
    public partial string? ConnectionError { get; set; }

    public SshConnection(Server server) : this(server, new KnownHostsStore())
    {
    }

    public SshConnection(Server server, KnownHostsStore knownHosts)
    {
        _server = server;
        _knownHosts = knownHosts;
    }

    /// Ends every in-flight command and refuses every future one against
    /// this connection - the equivalent of closing the window mid-operation
    /// and expecting the SSH work backing it to actually stop, not just be
    /// abandoned locally while it keeps running server-side.
    public void TerminateAll()
    {
        List<SshCommand> commands;
        SshClient? client;
        lock (_gate)
        {
            _invalidated = true;
            commands = [.. _activeCommands];
            _activeCommands.Clear();
            client = _client;
            _client = null;
        }

        foreach (var command in commands)
        {
            CancelAndDispose(command);
        }

        if (client is not null)
        {
            try
            {
                client.Disconnect();
            }
            catch (Exception)
            {
                // Best-effort: the connection is being torn down regardless.
            }
            client.Dispose();
        }
    }

    private static void CancelAndDispose(SshCommand command)
    {
        try
        {
            command.CancelAsync(true, 1000);
        }
        catch (Exception)
        {
            // Best-effort: the command is being abandoned regardless.
        }
        command.Dispose();
    }

    public async Task TestConnectionAsync(CancellationToken cancellationToken = default)
    {
        await ConnectAsync(cancellationToken).ConfigureAwait(false);
        var output = await ExecuteCommandAsync("echo 'connection_ok'", cancellationToken: cancellationToken).ConfigureAwait(false);
        if (!output.Trim().EndsWith("connection_ok", StringComparison.Ordinal))
        {
            throw new SshConnectionFailedException($"Unexpected response: {output}");
        }
        IsConnected = true;
        ConnectionError = null;
    }

    private async Task ConnectAsync(CancellationToken cancellationToken)
    {
        var connectionInfo = BuildConnectionInfo();
        var client = new SshClient(connectionInfo);
        client.HostKeyReceived += OnHostKeyReceived;
        try
        {
            await client.ConnectAsync(cancellationToken).ConfigureAwait(false);
        }
        catch (SshAuthenticationException ex)
        {
            client.Dispose();
            throw new SshConnectionFailedException($"認証に失敗しました (Permission denied): {ex.Message}");
        }
        catch (SshOperationTimeoutException)
        {
            client.Dispose();
            throw new SshConnectionFailedException("接続がタイムアウトしました");
        }
        catch (SocketException ex) when (ex.SocketErrorCode is SocketError.HostNotFound or SocketError.HostUnreachable or SocketError.NetworkUnreachable)
        {
            client.Dispose();
            throw new SshConnectionFailedException("ホストに到達できません");
        }
        catch (SshHostKeyMismatchException)
        {
            client.Dispose();
            throw;
        }
        catch (Exception ex)
        {
            client.Dispose();
            throw new SshConnectionFailedException(ex.Message);
        }

        lock (_gate)
        {
            if (_invalidated)
            {
                client.Dispose();
                throw new OperationCanceledException();
            }
            _client = client;
        }
    }

    private void OnHostKeyReceived(object? sender, HostKeyEventArgs e)
    {
        e.CanTrust = _knownHosts.TrustAndRecord(_server.Hostname, _server.Port, e.FingerPrintSHA256);
        if (!e.CanTrust)
        {
            // Thrown from inside the event handler: SSH.NET propagates this
            // out of Connect()/ConnectAsync() as the connection's failure.
            throw new SshHostKeyMismatchException(_server.Hostname);
        }
    }

    private ConnectionInfo BuildConnectionInfo()
    {
        var authMethods = new List<AuthenticationMethod>();
        if (_server.AuthType == AuthType.SshKey)
        {
            var keyPath = ResolveKeyPath();
            var keyFile = new PrivateKeyFile(keyPath);
            authMethods.Add(new PrivateKeyAuthenticationMethod(_server.Username, keyFile));
        }

        return new ConnectionInfo(_server.Hostname, _server.Port, _server.Username, [.. authMethods])
        {
            Timeout = TimeSpan.FromSeconds(10),
        };
    }

    /// SSH.NET has no equivalent of a bare `ssh` invocation's own default key
    /// discovery (try id_ed25519, then id_rsa, ... or ask an agent) - an
    /// explicit key path must be handed to it. When the saved server didn't
    /// specify one, this approximates that discovery by trying the same
    /// well-known filenames a real SSH client tries first, in the same
    /// order.
    private string ResolveKeyPath()
    {
        if (!string.IsNullOrWhiteSpace(_server.KeyPath))
        {
            return _server.KeyPath;
        }

        var sshDirectory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ssh");
        foreach (var candidate in new[] { "id_ed25519", "id_ecdsa", "id_rsa" })
        {
            var path = Path.Combine(sshDirectory, candidate);
            if (File.Exists(path))
            {
                return path;
            }
        }

        throw new SshKeyNotFoundException(Path.Combine(sshDirectory, "id_ed25519"));
    }

    public async Task<string> ExecuteCommandAsync(
        string command,
        TimeSpan? timeout = null,
        int outputLimit = DefaultOutputLimit,
        CancellationToken cancellationToken = default)
    {
        var boundedTimeout = BoundTimeout(timeout);

        SshClient client;
        lock (_gate)
        {
            if (_invalidated || _client is null)
            {
                throw new OperationCanceledException();
            }
            client = _client;
        }

        var sshCommand = client.CreateCommand(command);
        lock (_gate)
        {
            if (_invalidated)
            {
                sshCommand.Dispose();
                throw new OperationCanceledException();
            }
            _activeCommands.Add(sshCommand);
        }

        try
        {
            using var timeoutCts = new CancellationTokenSource(boundedTimeout);
            using var linkedCts = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken, timeoutCts.Token);

            var stdout = new BoundedBuffer(outputLimit);
            var stderr = new BoundedBuffer(ErrorOutputLimit);

            Task executeTask;
            Task stdoutTask;
            Task stderrTask;
            try
            {
                executeTask = sshCommand.ExecuteAsync(linkedCts.Token);
                stdoutTask = DrainAsync(sshCommand.OutputStream, stdout, linkedCts.Token);
                stderrTask = DrainAsync(sshCommand.ExtendedOutputStream, stderr, linkedCts.Token);
            }
            catch (Exception ex)
            {
                throw new SshConnectionFailedException(ex.Message);
            }

            try
            {
                await Task.WhenAll(executeTask, stdoutTask, stderrTask).ConfigureAwait(false);
            }
            catch (OperationCanceledException) when (timeoutCts.IsCancellationRequested)
            {
                throw new SshCommandTimedOutException();
            }
            catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
            {
                throw;
            }

            // stderr is only used below, on the failure path, so an
            // oversized stderr must not discard stdout - the actual payload
            // - on an otherwise-successful command. stdout is still held to
            // its own limit unconditionally.
            if (stdout.Exceeded)
            {
                throw new SshOutputTooLargeException();
            }

            if (sshCommand.ExitStatus == 0)
            {
                return stdout.ToUtf8String();
            }

            var message = stderr.ToUtf8String().Trim();
            if (message.Length == 0)
            {
                message = stdout.ToUtf8String().Trim();
                if (message.Length == 0)
                {
                    message = $"Unknown error (Exit code: {sshCommand.ExitStatus})";
                }
            }
            else if (stderr.Exceeded)
            {
                message += "\n…(truncated)";
            }

            if (message.Contains("command not found", StringComparison.Ordinal)
                || (message.Contains("No such file or directory", StringComparison.Ordinal) && message.Contains("file-exploder", StringComparison.Ordinal)))
            {
                message = $"サーバーに file-exploder がインストールされていないか、PATHが通っていません。\n詳細: {message}";
            }

            throw new SshCommandFailedException(message);
        }
        finally
        {
            lock (_gate)
            {
                _activeCommands.Remove(sshCommand);
            }
            sshCommand.Dispose();
        }
    }

    /// Reads a stream to EOF into `sink`, stopping early once `sink`'s limit
    /// is exceeded - the stream keeps being drained past that point (via the
    /// same "read and discard" the Swift client's own drain loop uses) so
    /// the remote side is never left blocked writing into a channel nobody
    /// is reading from.
    private static async Task DrainAsync(Stream stream, BoundedBuffer sink, CancellationToken cancellationToken)
    {
        var buffer = new byte[ReadBufferSize];
        int read;
        while ((read = await stream.ReadAsync(buffer.AsMemory(0, buffer.Length), cancellationToken).ConfigureAwait(false)) > 0)
        {
            sink.Append(buffer.AsSpan(0, read));
        }
    }

    private static TimeSpan BoundTimeout(TimeSpan? timeout)
    {
        if (timeout is not { } value || !double.IsFinite(value.TotalSeconds))
        {
            return TimeSpan.FromSeconds(120);
        }
        var seconds = Math.Clamp(value.TotalSeconds, 1, 86_400);
        return TimeSpan.FromSeconds(seconds);
    }

    public async Task<T> ExecuteJsonCommandAsync<T>(string command, CancellationToken cancellationToken = default)
    {
        var output = await ExecuteCommandAsync(command, cancellationToken: cancellationToken).ConfigureAwait(false);
        try
        {
            return System.Text.Json.JsonSerializer.Deserialize<T>(output)
                ?? throw new SshInvalidResponseException("empty result");
        }
        catch (System.Text.Json.JsonException ex)
        {
            throw new SshInvalidResponseException(ex.Message);
        }
    }
}

/// A byte sink with a hard cap: bytes past the limit are discarded (not
/// buffered and trimmed afterward, which would still pay for holding them
/// all in memory first), and the sink remembers that it was truncated.
/// Mirrors the Swift client's SendableData.
internal sealed class BoundedBuffer(int limit)
{
    private readonly MemoryStream _buffer = new();
    private bool _exceeded;

    public bool Exceeded => _exceeded;

    public void Append(ReadOnlySpan<byte> data)
    {
        if (_exceeded)
        {
            return;
        }
        var remaining = limit - (int)_buffer.Length;
        if (remaining <= 0)
        {
            _exceeded = true;
            return;
        }
        if (data.Length > remaining)
        {
            _buffer.Write(data[..remaining]);
            _exceeded = true;
        }
        else
        {
            _buffer.Write(data);
        }
    }

    public string ToUtf8String() => Encoding.UTF8.GetString(_buffer.GetBuffer(), 0, (int)_buffer.Length);
}
