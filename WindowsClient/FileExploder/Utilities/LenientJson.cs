using System.Text.Json;

namespace FileExploder.Utilities;

/// Decodes a JSON array element-by-element, keeping only the elements that
/// parse - an unrecognized enum value from an older or newer version of the
/// app or server, say - instead of losing every element to one bad entry.
/// Mirrors the Swift client's FailableDecodable&lt;T&gt; wrapper.
public static class LenientJson
{
    public static List<T> DeserializeLenientArray<T>(string json, JsonSerializerOptions options)
    {
        using var document = JsonDocument.Parse(json);
        return DeserializeLenientArray<T>(document.RootElement, options);
    }

    public static List<T> DeserializeLenientArray<T>(JsonElement arrayElement, JsonSerializerOptions options)
    {
        // EnumerateArray throws InvalidOperationException - not JsonException -
        // on anything that isn't an array, which slips straight past every
        // caller here: they all catch JsonException, because "the document is
        // not what we asked for" is what that means to them. A servers.json
        // holding `{}` or `null` (hand-edited, or corrupted into something
        // that still parses) would take the whole window down rather than
        // being reported as an unreadable server list.
        if (arrayElement.ValueKind != JsonValueKind.Array)
        {
            throw new JsonException($"expected a JSON array, got {arrayElement.ValueKind}");
        }

        var results = new List<T>();
        foreach (var element in arrayElement.EnumerateArray())
        {
            try
            {
                if (element.Deserialize<T>(options) is { } value)
                {
                    results.Add(value);
                }
            }
            catch (JsonException)
            {
                // This one element didn't decode; the rest of the array is
                // still good, so keep going instead of failing the whole
                // response.
            }
        }
        return results;
    }
}
