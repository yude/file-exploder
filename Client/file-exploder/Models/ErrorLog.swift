import Foundation

/// The two kinds of error the file list can be showing, kept apart because they
/// have different lifetimes.
///
/// `FileListViewModel` is a `@MainActor` class, not a lock: every `await`
/// inside an operation is a point where another operation can run. With a
/// single error field, two overlapping operations fought over it — and because
/// every listing clears the field before it starts, an unrelated refresh
/// (another operation finishing, the settings observer, the retry button) could
/// erase a failure the user had not seen yet.
///
/// So: operation errors accumulate and survive a listing; the listing error is
/// replaced by each listing. Only a deliberate reload or a navigation retires
/// the operation errors.
struct ErrorLog: Equatable {
    /// How many operation errors are kept.
    ///
    /// A bulk operation reports one per file it could not finish, and there is
    /// no bound on how many files that is - delete everything in a directory
    /// the user has no write permission on and every single one fails. All of
    /// them were then joined into the single string the error banner renders,
    /// so a few thousand selected files meant hundreds of kilobytes of text in
    /// one label: not readable by anyone, and not something a UI toolkit lays
    /// out quickly. Past this many the rest are counted rather than kept - the
    /// first ones name the problem, and the count says how far it goes.
    static let maxOperationErrors = 20

    private(set) var operationErrors: [String] = []
    /// How many operation errors maxOperationErrors left out.
    private(set) var droppedOperationErrors = 0
    private(set) var listingError: String?

    mutating func addOperationError(_ message: String) {
        addOperationErrors([message])
    }

    mutating func addOperationErrors(_ messages: [String]) {
        for message in messages {
            if operationErrors.count < Self.maxOperationErrors {
                operationErrors.append(message)
            } else {
                droppedOperationErrors += 1
            }
        }
    }

    mutating func clearOperationErrors() {
        operationErrors.removeAll()
        droppedOperationErrors = 0
    }

    mutating func setListingError(_ message: String?) {
        listingError = message
    }

    mutating func removeAll() {
        clearOperationErrors()
        listingError = nil
    }

    /// The kept errors, plus a line accounting for any the cap left out.
    private var operationMessages: [String] {
        guard droppedOperationErrors > 0 else { return operationErrors }
        return operationErrors + ["ほか \(droppedOperationErrors) 件のエラー"]
    }

    /// The operation errors alone. These belong beside the file list, not
    /// instead of it: the listing succeeded, one action within it did not.
    var operationMessage: String? {
        operationMessages.isEmpty ? nil : operationMessages.joined(separator: "\n")
    }

    /// What the view shows, or nil when there is nothing to report. A listing
    /// error reads on its own; alongside operation errors it is labelled, so it
    /// is clear which part failed.
    var message: String? {
        var messages = operationMessages
        if let listingError {
            messages.append(operationErrors.isEmpty ? listingError : "一覧更新エラー: \(listingError)")
        }
        return messages.isEmpty ? nil : messages.joined(separator: "\n")
    }
}
