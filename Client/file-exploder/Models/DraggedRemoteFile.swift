import CoreTransferable
import Foundation

/// A row being dragged.
///
/// It travels as its remote path in plain text. That keeps the drag useful
/// outside the table - dropping a row into any text field yields the path - and
/// it means a drag that started somewhere else in the system arrives as a string
/// this app simply fails to recognise, rather than as something it might act on.
/// Every drop resolves the paths against the rows currently listed and ignores
/// whatever does not match.
struct DraggedRemoteFile: Transferable {
    let path: String

    static var transferRepresentation: some TransferRepresentation {
        ProxyRepresentation(exporting: \.path, importing: DraggedRemoteFile.init(path:))
    }
}
