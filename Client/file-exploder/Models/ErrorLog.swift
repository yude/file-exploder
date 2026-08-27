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
    private(set) var operationErrors: [String] = []
    private(set) var listingError: String?

    mutating func addOperationError(_ message: String) {
        operationErrors.append(message)
    }

    mutating func addOperationErrors(_ messages: [String]) {
        operationErrors.append(contentsOf: messages)
    }

    mutating func clearOperationErrors() {
        operationErrors.removeAll()
    }

    mutating func setListingError(_ message: String?) {
        listingError = message
    }

    mutating func removeAll() {
        operationErrors.removeAll()
        listingError = nil
    }

    /// What the view shows, or nil when there is nothing to report. A listing
    /// error reads on its own; alongside operation errors it is labelled, so it
    /// is clear which part failed.
    var message: String? {
        var messages = operationErrors
        if let listingError {
            messages.append(operationErrors.isEmpty ? listingError : "一覧更新エラー: \(listingError)")
        }
        return messages.isEmpty ? nil : messages.joined(separator: "\n")
    }
}
