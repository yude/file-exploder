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
