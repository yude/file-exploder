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
    /// How many operation errors are kept.
    ///
    /// A bulk operation reports one per file it could not finish, and there is
    /// no bound on how many files that is - delete everything in a directory
    /// the user has no write permission on and every single one fails. All of
    /// them were then joined into the single string the error banner renders,
    /// so a few thousand selected files meant hundreds of kilobytes of text in
    /// one TextBlock: not readable by anyone, and not something a UI toolkit
    /// lays out quickly. Past this many the rest are counted rather than kept
    /// - the first ones name the problem, and the count says how far it goes.
    public const int MaxOperationErrors = 20;

    public IReadOnlyList<string> OperationErrors { get; init; } = [];

    /// How many operation errors MaxOperationErrors left out.
    public int DroppedOperationErrors { get; init; }

    public string? ListingError { get; init; }

    public ErrorLog AddOperationError(string message) => AddOperationErrors([message]);

    public ErrorLog AddOperationErrors(IEnumerable<string> messages)
    {
        var kept = new List<string>(OperationErrors);
        var dropped = DroppedOperationErrors;
        foreach (var message in messages)
        {
            if (kept.Count < MaxOperationErrors)
            {
                kept.Add(message);
            }
            else
            {
                dropped++;
            }
        }
        return this with { OperationErrors = kept, DroppedOperationErrors = dropped };
    }

    public ErrorLog ClearOperationErrors() =>
        this with { OperationErrors = [], DroppedOperationErrors = 0 };

    public ErrorLog SetListingError(string? message) =>
        this with { ListingError = message };

    public ErrorLog Clear() => new();

    /// The kept errors, plus a line accounting for any the cap left out.
    private List<string> OperationMessages()
    {
        var messages = new List<string>(OperationErrors);
        if (DroppedOperationErrors > 0)
        {
            messages.Add($"ほか {DroppedOperationErrors} 件のエラー");
        }
        return messages;
    }

    /// The operation errors alone. These belong beside the file list, not
    /// instead of it: the listing succeeded, one action within it did not.
    public string? OperationMessage
    {
        get
        {
            var messages = OperationMessages();
            return messages.Count == 0 ? null : string.Join('\n', messages);
        }
    }

    /// What the view shows, or null when there is nothing to report. A
    /// listing error reads on its own; alongside operation errors it is
    /// labelled, so it is clear which part failed.
    public string? Message
    {
        get
        {
            var messages = OperationMessages();
            if (ListingError is { } listingError)
            {
                messages.Add(OperationErrors.Count == 0 ? listingError : $"一覧更新エラー: {listingError}");
            }
            return messages.Count == 0 ? null : string.Join('\n', messages);
        }
    }
}
