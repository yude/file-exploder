using Avalonia.Input;

namespace FileExploder.Views;

/// The in-process drag payload shared by the file table's rows and the
/// breadcrumb's crumbs - one dragged file's remote path. Mirrors the macOS
/// client's DraggedRemoteFile Transferable: the payload is always a single
/// path, even when the user is dragging a multi-row selection; the drop
/// side decides whether to widen it to the whole selection (see
/// FileListView.ResolveDropSources).
///
/// An in-process format (not text/a filename) so it never matches a drag
/// coming from outside the app - an external file drag onto a directory
/// row falls through to whatever default handling that row has, instead of
/// being misread as a remote path.
public static class RemoteFileDrag
{
    public static readonly DataFormat<string> Format = DataFormat.CreateInProcessFormat<string>("application/x-file-exploder-remote-path");
}
