namespace FileExploder.Services;

public abstract class SshClientException : Exception
{
    protected SshClientException(string message) : base(message)
    {
    }
}

public sealed class SshConnectionFailedException : SshClientException
{
    public SshConnectionFailedException(string reason) : base($"接続に失敗しました: {reason}")
    {
    }
}

public sealed class SshCommandFailedException : SshClientException
{
    public SshCommandFailedException(string reason) : base(reason)
    {
    }
}

public sealed class SshCommandTimedOutException : SshClientException
{
    public SshCommandTimedOutException() : base("リモートコマンドがタイムアウトしました")
    {
    }
}

public sealed class SshOutputTooLargeException : SshClientException
{
    public SshOutputTooLargeException() : base("リモートコマンドの出力が大きすぎます")
    {
    }
}

public sealed class SshInvalidResponseException : SshClientException
{
    public SshInvalidResponseException(string detail) : base($"サーバーからの応答が不正です: {detail}")
    {
    }
}

public sealed class SshKeyNotFoundException : SshClientException
{
    public SshKeyNotFoundException(string path) : base($"SSHキーが見つかりません: {path}")
    {
    }
}

/// The host key presented for this connection does not match the one
/// recorded the first time this app connected to it - possibly a
/// man-in-the-middle, possibly the server was legitimately reinstalled or
/// its key rotated. Either way, this is refused rather than silently
/// accepted, mirroring the macOS client's use of StrictHostKeyChecking's
/// "accept-new" mode (trust a host's key the first time, but never a
/// changed one afterward).
public sealed class SshHostKeyMismatchException : SshClientException
{
    public SshHostKeyMismatchException(string host)
        : base($"{host} のホストキーが以前と異なります。サーバーが変更された可能性があります。心当たりがない場合は接続しないでください。")
    {
    }
}
