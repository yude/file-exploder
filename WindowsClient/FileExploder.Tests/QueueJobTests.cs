using FileExploder.Models;

namespace FileExploder.Tests;

public class QueueJobClipboardLogTests
{
    [Fact]
    public void IncludesAllUsefulJobDetails()
    {
        var job = new QueueJob
        {
            Id = "job-123",
            Type = OperationType.Copy,
            SrcPath = "/srv/source.txt",
            DstPath = "/srv/destination.txt",
            Status = JobStatus.Failed,
            Error = "permission denied",
            CreatedAt = DateTimeOffset.FromUnixTimeSeconds(0),
            StartedAt = DateTimeOffset.FromUnixTimeSeconds(1),
            CompletedAt = DateTimeOffset.FromUnixTimeSeconds(2),
        };

        Assert.Equal(
            """
            ID: job-123
            操作: コピー
            状態: 失敗
            内容: コピー /srv/source.txt -> /srv/destination.txt
            作成日時: 1970-01-01T00:00:00Z
            開始日時: 1970-01-01T00:00:01Z
            完了日時: 1970-01-01T00:00:02Z
            エラー: permission denied
            """,
            job.ClipboardLog);
    }

    [Fact]
    public void OmitsUnavailableOptionalDetails()
    {
        var job = new QueueJob
        {
            Id = "job-456",
            Type = OperationType.Mkdir,
            DstPath = "/srv/new-directory",
            Status = JobStatus.Pending,
            CreatedAt = DateTimeOffset.FromUnixTimeSeconds(0),
        };

        Assert.DoesNotContain("開始日時:", job.ClipboardLog);
        Assert.DoesNotContain("完了日時:", job.ClipboardLog);
        Assert.DoesNotContain("エラー:", job.ClipboardLog);
        Assert.Contains("内容: 作成 /srv/new-directory", job.ClipboardLog);
    }
}
