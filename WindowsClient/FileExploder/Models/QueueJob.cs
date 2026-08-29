using System.Globalization;
using System.Text.Json;
using System.Text.Json.Serialization;
using FileExploder.Utilities;

namespace FileExploder.Models;

/// The daemon serialises jobs with snake_case keys (see `queue.Job` in the
/// Go server), so the mapping has to be spelled out via JsonPropertyName.
public sealed record QueueJob
{
    [JsonPropertyName("id")]
    public required string Id { get; init; }

    [JsonPropertyName("type")]
    public required OperationType Type { get; init; }

    [JsonPropertyName("src_path")]
    public string? SrcPath { get; init; }

    [JsonPropertyName("dst_path")]
    public string? DstPath { get; init; }

    [JsonPropertyName("mode")]
    public string? Mode { get; init; }

    [JsonPropertyName("status")]
    public required JobStatus Status { get; init; }

    [JsonPropertyName("error")]
    public string? Error { get; init; }

    [JsonPropertyName("created_at")]
    [JsonConverter(typeof(Rfc3339DateTimeOffsetConverter))]
    public required DateTimeOffset CreatedAt { get; init; }

    [JsonPropertyName("started_at")]
    [JsonConverter(typeof(Rfc3339NullableDateTimeOffsetConverter))]
    public DateTimeOffset? StartedAt { get; init; }

    [JsonPropertyName("completed_at")]
    [JsonConverter(typeof(Rfc3339NullableDateTimeOffsetConverter))]
    public DateTimeOffset? CompletedAt { get; init; }

    public string Description => Type switch
    {
        OperationType.Rename => $"名前変更 {SrcPath ?? ""} -> {DstPath ?? ""}",
        OperationType.Move => $"移動 {SrcPath ?? ""} -> {DstPath ?? ""}",
        OperationType.Delete => $"削除 {SrcPath ?? ""}",
        OperationType.Copy => $"コピー {SrcPath ?? ""} -> {DstPath ?? ""}",
        OperationType.Mkdir => $"作成 {DstPath ?? ""}",
        OperationType.Chmod => $"権限変更 {DstPath ?? ""} to {Mode ?? ""}",
        _ => throw new ArgumentOutOfRangeException(nameof(Type)),
    };

    /// A stable, self-contained representation suitable for pasting into an
    /// issue or a support message. Unlike the compact row, this deliberately
    /// includes the job ID and every available timestamp.
    public string ClipboardLog
    {
        get
        {
            var lines = new List<string>
            {
                $"ID: {Id}",
                $"操作: {Type.DisplayName()}",
                $"状態: {Status.DisplayName()}",
                $"内容: {Description}",
                $"作成日時: {FormatLogDate(CreatedAt)}",
            };

            if (StartedAt is { } startedAt)
            {
                lines.Add($"開始日時: {FormatLogDate(startedAt)}");
            }
            if (CompletedAt is { } completedAt)
            {
                lines.Add($"完了日時: {FormatLogDate(completedAt)}");
            }
            if (!string.IsNullOrEmpty(Error))
            {
                lines.Add($"エラー: {Error}");
            }

            return string.Join('\n', lines);
        }
    }

    private static string FormatLogDate(DateTimeOffset date) =>
        date.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", CultureInfo.InvariantCulture);
}

[JsonConverter(typeof(OperationTypeJsonConverter))]
public enum OperationType
{
    Rename,
    Move,
    Delete,
    Copy,
    Mkdir,
    Chmod,
}

/// Matches the Go server's job type strings exactly and, like the Swift
/// client, rejects anything it doesn't recognise rather than silently
/// mapping it to a default - a job with an unrecognised type is dropped by
/// the lenient array-level decode (see FailableDecodeList) instead of
/// hiding every other job in the same response.
public sealed class OperationTypeJsonConverter : JsonConverter<OperationType>
{
    public override OperationType Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options) =>
        reader.GetString() switch
        {
            "rename" => OperationType.Rename,
            "move" => OperationType.Move,
            "delete" => OperationType.Delete,
            "copy" => OperationType.Copy,
            "mkdir" => OperationType.Mkdir,
            "chmod" => OperationType.Chmod,
            var raw => throw new JsonException($"Unknown operation type: {raw}"),
        };

    public override void Write(Utf8JsonWriter writer, OperationType value, JsonSerializerOptions options) =>
        writer.WriteStringValue(value switch
        {
            OperationType.Rename => "rename",
            OperationType.Move => "move",
            OperationType.Delete => "delete",
            OperationType.Copy => "copy",
            OperationType.Mkdir => "mkdir",
            OperationType.Chmod => "chmod",
            _ => throw new ArgumentOutOfRangeException(nameof(value), value, message: null),
        });
}

public static class OperationTypeExtensions
{
    public static string DisplayName(this OperationType type) => type switch
    {
        OperationType.Rename => "名前変更",
        OperationType.Move => "移動",
        OperationType.Delete => "削除",
        OperationType.Copy => "コピー",
        OperationType.Mkdir => "新規フォルダ",
        OperationType.Chmod => "権限変更",
        _ => throw new ArgumentOutOfRangeException(nameof(type), type, message: null),
    };
}

[JsonConverter(typeof(JobStatusJsonConverter))]
public enum JobStatus
{
    Pending,
    Running,
    Completed,
    Failed,
    Cancelled,
}

/// See OperationTypeJsonConverter - same reasoning, same strict/no-fallback
/// behavior, applied to job status.
public sealed class JobStatusJsonConverter : JsonConverter<JobStatus>
{
    public override JobStatus Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options) =>
        reader.GetString() switch
        {
            "pending" => JobStatus.Pending,
            "running" => JobStatus.Running,
            "completed" => JobStatus.Completed,
            "failed" => JobStatus.Failed,
            "cancelled" => JobStatus.Cancelled,
            var raw => throw new JsonException($"Unknown job status: {raw}"),
        };

    public override void Write(Utf8JsonWriter writer, JobStatus value, JsonSerializerOptions options) =>
        writer.WriteStringValue(value switch
        {
            JobStatus.Pending => "pending",
            JobStatus.Running => "running",
            JobStatus.Completed => "completed",
            JobStatus.Failed => "failed",
            JobStatus.Cancelled => "cancelled",
            _ => throw new ArgumentOutOfRangeException(nameof(value), value, message: null),
        });
}

public static class JobStatusExtensions
{
    public static string DisplayName(this JobStatus status) => status switch
    {
        JobStatus.Pending => "待機中",
        JobStatus.Running => "実行中",
        JobStatus.Completed => "完了",
        JobStatus.Failed => "失敗",
        JobStatus.Cancelled => "キャンセル",
        _ => throw new ArgumentOutOfRangeException(nameof(status), status, message: null),
    };
}
