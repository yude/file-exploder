using System.Globalization;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace FileExploder.Utilities;

/// Parses the RFC 3339 timestamps the Go daemon emits.
///
/// Go's `time.Time` marshals with up to nine fractional digits (and trims
/// trailing zeros). The Swift client needs a hand-rolled fallback for this,
/// because Foundation's ISO8601DateFormatter only reliably accepts three
/// fractional digits and rejects the rest outright. .NET's DateTimeOffset
/// parser has no such limit - it accepts anywhere from zero to nine
/// fractional digits natively (rounding to its own 100ns tick resolution),
/// so no equivalent workaround is needed here; this converter is a thin
/// wrapper purely to plug that parser into System.Text.Json.
public sealed class Rfc3339DateTimeOffsetConverter : JsonConverter<DateTimeOffset>
{
    public override DateTimeOffset Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        var value = reader.GetString();
        if (value is null || !DateTimeOffset.TryParse(value, CultureInfo.InvariantCulture, DateTimeStyles.None, out var result))
        {
            throw new JsonException($"Invalid RFC 3339 date: {value}");
        }
        return result;
    }

    public override void Write(Utf8JsonWriter writer, DateTimeOffset value, JsonSerializerOptions options) =>
        writer.WriteStringValue(value.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss.fffffff'Z'", CultureInfo.InvariantCulture));
}

/// The nullable counterpart, for optional timestamp fields (started_at,
/// completed_at) that are genuinely absent rather than merely unparsable.
public sealed class Rfc3339NullableDateTimeOffsetConverter : JsonConverter<DateTimeOffset?>
{
    public override DateTimeOffset? Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        if (reader.TokenType == JsonTokenType.Null)
        {
            return null;
        }
        var value = reader.GetString();
        if (value is null || !DateTimeOffset.TryParse(value, CultureInfo.InvariantCulture, DateTimeStyles.None, out var result))
        {
            throw new JsonException($"Invalid RFC 3339 date: {value}");
        }
        return result;
    }

    public override void Write(Utf8JsonWriter writer, DateTimeOffset? value, JsonSerializerOptions options)
    {
        if (value is null)
        {
            writer.WriteNullValue();
        }
        else
        {
            writer.WriteStringValue(value.Value.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss.fffffff'Z'", CultureInfo.InvariantCulture));
        }
    }
}
