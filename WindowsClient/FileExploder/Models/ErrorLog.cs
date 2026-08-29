namespace FileExploder.Models;

/// The two kinds of error the file list can be showing, kept apart because
/// they have different lifetimes.
///
/// FileListViewModel's own methods are `async`, not synchronized: every
/// `await` inside an operation is a point where another operation can run.
/// With a single error field, two overlapping operations would fight over
/// it - and because every listing clears the field before it starts, an
/// unrelated refresh (another operation finishing, a settings change, the
/// retry button) could erase a failure the user had not seen yet.
///
/// So: operation errors accumulate and survive a listing; the listing error
/// is replaced by each listing. Only a deliberate reload or a navigation
/// retires the operation errors.
///
/// Immutable, like a Swift value type: every "mutation" below returns a new
/// instance rather than changing this one in place, so assigning the result
/// back to an `[ObservableProperty]`-backed field is what actually raises
/// the change notification - the same way reassigning a `@Published` Swift
/// struct property does.
public sealed record ErrorLog
{
    public IReadOnlyList<string> OperationErrors { get; init; } = [];
    public string? ListingError { get; init; }

    public ErrorLog AddOperationError(string message) =>
        this with { OperationErrors = [.. OperationErrors, message] };

    public ErrorLog AddOperationErrors(IEnumerable<string> messages) =>
        this with { OperationErrors = [.. OperationErrors, .. messages] };

    public ErrorLog ClearOperationErrors() =>
        this with { OperationErrors = [] };

    public ErrorLog SetListingError(string? message) =>
        this with { ListingError = message };

    public ErrorLog Clear() => new();

    /// The operation errors alone. These belong beside the file list, not
    /// instead of it: the listing succeeded, one action within it did not.
    public string? OperationMessage =>
        OperationErrors.Count == 0 ? null : string.Join('\n', OperationErrors);

    /// What the view shows, or null when there is nothing to report. A
    /// listing error reads on its own; alongside operation errors it is
    /// labelled, so it is clear which part failed.
    public string? Message
    {
        get
        {
            var messages = new List<string>(OperationErrors);
            if (ListingError is { } listingError)
            {
                messages.Add(OperationErrors.Count == 0 ? listingError : $"一覧更新エラー: {listingError}");
            }
            return messages.Count == 0 ? null : string.Join('\n', messages);
        }
    }
}
