using System.ComponentModel;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Data.Converters;
using Avalonia.Input;
using Avalonia.Input.Platform;
using Avalonia.Interactivity;
using Avalonia.Media;
using FileExploder.Models;
using FileExploder.Utilities;
using FileExploder.ViewModels;

namespace FileExploder.Views;

public static class FileListConverters
{
    public static readonly IValueConverter FileToIcon = new FuncValueConverter<RemoteFile?, string>(f => f is null ? "" : FileIcons.Icon(f));
}

/// The file table, its toolbar, its breadcrumb and every dialog a file
/// operation can open. Ports FileListView.swift; see the class-level
/// comments on FileListViewModel for the operation logic this only invokes.
public partial class FileListView : UserControl
{
    private static readonly IBrush DropHighlightBrush = new SolidColorBrush(Colors.CornflowerBlue, 0.22);

    private FileListViewModel _vm = null!;
    private HashSet<string> _selectedIds = [];
    private string _searchText = "";
    private List<RemoteFile> _lastComputedFiles = [];

    internal DataGrid FilesGridForTesting => FilesGrid;

    /// Set the first time a drag actually starts, so a test can tell
    /// "the press was observed and the threshold was crossed" apart from
    /// "nothing happened" - DoDragDropAsync itself takes over the pointer
    /// and leaves no other observable trace in a headless run.
    internal bool DragWasInitiatedForTesting { get; private set; }

    // Column sorting is handled entirely here, not by the DataGrid's own
    // built-in sort machinery: re-deriving the sorted list ourselves from
    // FilesViewModel.Files + the search text + whichever column the user
    // last clicked is the only way this survives a background auto-refresh
    // replacing the grid's ItemsSource every few seconds without silently
    // reverting the user's chosen sort order.
    private DataGridColumn? _sortColumn;
    private ListSortDirection _sortDirection = ListSortDirection.Ascending;
    private readonly Dictionary<DataGridColumn, Func<RemoteFile, IComparable>> _sortKeySelectors = new();

    // Row drag tracking. Deliberately does not mark the initiating
    // PointerPressed as handled, so the DataGrid's own row-selection and
    // double-click handling still sees it - putting a gesture in front of
    // the grid's own handling is exactly what broke selection and
    // double-click in the macOS client the first time this was attempted
    // (see commit "restore row selection and double-click in the file
    // list"). The flip side of that same coin is how the press has to be
    // subscribed to at all: see the comment in OnLoadingRow.
    private RemoteFile? _dragCandidate;
    private Point? _dragStartPoint;
    private PointerPressedEventArgs? _dragPressArgs;

    public FileListView()
    {
        InitializeComponent();

        BackButton.Click += (_, _) => _ = _vm.GoBackAsync();
        ForwardButton.Click += (_, _) => _ = _vm.GoForwardAsync();
        UpButton.Click += (_, _) => _ = _vm.GoToParentAsync();
        NewFolderButton.Click += async (_, _) => await CreateFolderAsync();
        ReloadButton.Click += (_, _) => _ = _vm.ReloadAsync();
        RetryButton.Click += (_, _) => _ = _vm.ReloadAsync();
        DismissOperationErrorButton.Click += (_, _) => _vm.DismissOperationErrors();
        SearchBox.TextChanged += (_, _) =>
        {
            _searchText = SearchBox.Text ?? "";
            RenderFiles();
        };

        _sortKeySelectors[FilesGrid.Columns[0]] = f => f.Name;
        _sortKeySelectors[FilesGrid.Columns[1]] = f => f.Size;
        _sortKeySelectors[FilesGrid.Columns[2]] = f => f.ModificationDate;
        _sortKeySelectors[FilesGrid.Columns[3]] = f => f.SymbolicPermissions;
        foreach (var column in FilesGrid.Columns)
        {
            column.CanUserSort = false; // we resort ourselves; see the fields above
            column.HeaderPointerReleased += (_, _) => OnHeaderClicked(column);
        }

        FilesGrid.SelectionChanged += (_, _) =>
        {
            _selectedIds = [.. FilesGrid.SelectedItems.Cast<RemoteFile>().Select(f => f.Id)];
            UpdateSelectionCountText();
        };
        FilesGrid.LoadingRow += OnLoadingRow;
        FilesGrid.UnloadingRow += OnUnloadingRow;

        Breadcrumb.Navigated += path => _ = _vm.NavigateToAsync(path);
        Breadcrumb.DropRequested += (paths, destination) => MoveDropped(paths, destination);
    }

    public void Attach(FileListViewModel vm)
    {
        _vm = vm;
        DataContext = vm;
        vm.PropertyChanged += OnViewModelChanged;

        UpdateBreadcrumb();
        UpdateErrorBanner();
        UpdateToolbarState();
        RenderFiles();
    }

    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        switch (e.PropertyName)
        {
            case nameof(FileListViewModel.Files):
                RenderFiles();
                break;
            case nameof(FileListViewModel.CurrentPath):
                UpdateBreadcrumb();
                UpdateContentVisibility();
                break;
            case nameof(FileListViewModel.HasConnection):
                UpdateBreadcrumb();
                UpdateContentVisibility();
                break;
            case nameof(FileListViewModel.IsLoading):
                UpdateContentVisibility();
                break;
            case nameof(FileListViewModel.Errors):
                UpdateErrorBanner();
                UpdateContentVisibility();
                break;
        }
        UpdateToolbarState();
    }

    // MARK: - Rendering

    private void OnHeaderClicked(DataGridColumn column)
    {
        if (_sortColumn == column)
        {
            _sortDirection = _sortDirection == ListSortDirection.Ascending ? ListSortDirection.Descending : ListSortDirection.Ascending;
        }
        else
        {
            _sortColumn = column;
            _sortDirection = ListSortDirection.Ascending;
        }
        RenderFiles();
    }

    private List<RemoteFile> ComputeVisibleFiles()
    {
        IEnumerable<RemoteFile> files = _vm.Files;
        if (!string.IsNullOrEmpty(_searchText))
        {
            files = files.Where(f => f.Name.Contains(_searchText, StringComparison.CurrentCultureIgnoreCase));
        }
        if (_sortColumn is { } column && _sortKeySelectors.TryGetValue(column, out var key))
        {
            files = _sortDirection == ListSortDirection.Ascending ? files.OrderBy(key) : files.OrderByDescending(key);
        }
        return [.. files];
    }

    private void RenderFiles()
    {
        _lastComputedFiles = ComputeVisibleFiles();
        ApplyFiles(_lastComputedFiles);
        UpdateContentVisibility();
    }

    /// Restores selection by path after replacing ItemsSource (a fresh list
    /// of freshly-constructed RemoteFile instances every refresh) - without
    /// this, every auto-refresh tick would silently drop the user's
    /// selection, since the grid has no way to know the new instances refer
    /// to the same remote files.
    ///
    /// Reassigning ItemsSource synchronously clears SelectedItems, but the
    /// SelectionChanged notification for that clear is dispatcher-posted,
    /// not synchronous (verified empirically against this Avalonia version)
    /// - so it is never actually delivered mid-method here, since nothing
    /// below yields back to the dispatcher before this method returns.
    /// Reading `toRestore` from a snapshot taken *before* the reassignment,
    /// rather than from `_selectedIds` itself afterward, removes the
    /// dependency on that ordering detail rather than relying on it holding
    /// across Avalonia versions.
    private void ApplyFiles(List<RemoteFile> files)
    {
        var toRestore = _selectedIds;
        FilesGrid.ItemsSource = files;
        _selectedIds = [.. toRestore.Intersect(files.Select(f => f.Id))];
        FilesGrid.SelectedItems.Clear();
        foreach (var file in files.Where(f => _selectedIds.Contains(f.Id)))
        {
            FilesGrid.SelectedItems.Add(file);
        }
        UpdateSelectionCountText();
    }

    private void UpdateSelectionCountText()
    {
        // Counted against the rows on screen, not the raw selection: a
        // search can hide rows that stay selected, and every action goes
        // through the current on-screen targets only.
        var count = FilesGrid.SelectedItems.Count;
        SelectionCountText.Text = count > 0 ? $"{count} 項目を選択中" : "";
    }

    private void UpdateBreadcrumb() => Breadcrumb.Update(_vm.CurrentPath, _vm.RemoteRoot);

    private void UpdateErrorBanner()
    {
        var message = _vm.Errors.OperationMessage;
        OperationErrorBanner.IsVisible = message is not null;
        OperationErrorSeparator.IsVisible = message is not null;
        OperationErrorText.Text = message;
    }

    private void UpdateToolbarState()
    {
        BackButton.IsEnabled = _vm.CanGoBack && !_vm.IsLoading;
        ForwardButton.IsEnabled = _vm.CanGoForward && !_vm.IsLoading;
        UpButton.IsEnabled = _vm.CanGoToParent && !_vm.IsLoading;
        NewFolderButton.IsEnabled = _vm.HasConnection && !_vm.IsLoading;
        ReloadButton.IsEnabled = _vm.HasConnection && !_vm.IsLoading;
    }

    private void UpdateContentVisibility()
    {
        var hasConnection = _vm.HasConnection;
        var isLoading = _vm.IsLoading;
        var listingError = _vm.Errors.ListingError;
        var isEmpty = _lastComputedFiles.Count == 0;

        NoConnectionState.IsVisible = !hasConnection;
        LoadingState.IsVisible = hasConnection && isLoading;
        ListingErrorState.IsVisible = hasConnection && !isLoading && listingError is not null;
        if (listingError is not null)
        {
            ListingErrorText.Text = listingError;
        }
        EmptyDirectoryState.IsVisible = hasConnection && !isLoading && listingError is null && isEmpty;
        FilesGrid.IsVisible = hasConnection && !isLoading && listingError is null && !isEmpty;
    }

    // MARK: - Row drag and drop

    private void OnLoadingRow(object? sender, DataGridRowEventArgs e)
    {
        var row = e.Row;
        // AddHandler with handledEventsToo, not `row.PointerPressed +=`: the
        // DataGrid marks the press handled for its own row selection before
        // it ever reaches the row, so a plain subscription never fires at
        // all - verified directly. That one dead subscription is what broke
        // BOTH double-click-to-open and drag-to-move, since both hang off
        // it. Bubble (rather than Tunnel) so the grid still does its
        // selection first and this only observes afterward, which is what
        // the "not marking e.Handled" note in OnRowPointerPressed is about.
        // PointerMoved/PointerReleased are not handled by the grid and
        // arrive fine on a plain subscription.
        row.AddHandler(InputElement.PointerPressedEvent, OnRowPointerPressed, RoutingStrategies.Bubble, handledEventsToo: true);
        row.PointerMoved += OnRowPointerMoved;
        row.PointerReleased += OnRowPointerReleased;
        DragDrop.SetAllowDrop(row, true);
        row.AddHandler(DragDrop.DragOverEvent, OnRowDragOver);
        row.AddHandler(DragDrop.DragLeaveEvent, OnRowDragLeave);
        row.AddHandler(DragDrop.DropEvent, OnRowDrop);

        var menu = new ContextMenu();
        menu.Opening += (_, _) => PopulateContextMenu(menu, row);
        row.ContextMenu = menu;
    }

    private void OnUnloadingRow(object? sender, DataGridRowEventArgs e)
    {
        var row = e.Row;
        row.RemoveHandler(InputElement.PointerPressedEvent, OnRowPointerPressed);
        row.PointerMoved -= OnRowPointerMoved;
        row.PointerReleased -= OnRowPointerReleased;
        row.RemoveHandler(DragDrop.DragOverEvent, OnRowDragOver);
        row.RemoveHandler(DragDrop.DragLeaveEvent, OnRowDragLeave);
        row.RemoveHandler(DragDrop.DropEvent, OnRowDrop);
        row.ContextMenu = null;
    }

    private void OnRowPointerPressed(object? sender, PointerPressedEventArgs e)
    {
        var row = (DataGridRow)sender!;
        if (row.DataContext is not RemoteFile file)
        {
            return;
        }
        if (!e.GetCurrentPoint(row).Properties.IsLeftButtonPressed)
        {
            return;
        }

        if (e.ClickCount == 2)
        {
            if (file.IsDirectory)
            {
                _ = _vm.OpenFileAsync(file);
            }
            return; // a double-click never starts a drag
        }

        _dragCandidate = file;
        _dragStartPoint = e.GetPosition(this);
        _dragPressArgs = e;
        // Not marking e.Handled: the DataGrid's own selection handling for
        // this same press must still run.
    }

    private async void OnRowPointerMoved(object? sender, PointerEventArgs e)
    {
        if (_dragCandidate is null || _dragStartPoint is null || _dragPressArgs is null)
        {
            return;
        }
        if (!e.GetCurrentPoint((Control)sender!).Properties.IsLeftButtonPressed)
        {
            ResetDragTracking();
            return;
        }

        var delta = e.GetPosition(this) - _dragStartPoint.Value;
        const double threshold = 6;
        if (Math.Abs(delta.X) < threshold && Math.Abs(delta.Y) < threshold)
        {
            return;
        }

        var file = _dragCandidate;
        var pressArgs = _dragPressArgs;
        ResetDragTracking();

        DragWasInitiatedForTesting = true;
        var transfer = new DataTransfer();
        transfer.Add(DataTransferItem.Create(RemoteFileDrag.Format, file.Path));
        await DragDrop.DoDragDropAsync(pressArgs, transfer, DragDropEffects.Move);
    }

    private void OnRowPointerReleased(object? sender, PointerReleasedEventArgs e) => ResetDragTracking();

    private void ResetDragTracking()
    {
        _dragCandidate = null;
        _dragStartPoint = null;
        _dragPressArgs = null;
    }

    private void OnRowDragOver(object? sender, DragEventArgs e)
    {
        var row = (DataGridRow)sender!;
        var accepts = row.DataContext is RemoteFile { IsDirectory: true } && e.DataTransfer.Formats.Contains(RemoteFileDrag.Format);
        e.DragEffects = accepts ? DragDropEffects.Move : DragDropEffects.None;
        row.Background = accepts ? DropHighlightBrush : null;
    }

    private void OnRowDragLeave(object? sender, DragEventArgs e) => ((DataGridRow)sender!).Background = null;

    private void OnRowDrop(object? sender, DragEventArgs e)
    {
        var row = (DataGridRow)sender!;
        row.Background = null;
        if (row.DataContext is not RemoteFile { IsDirectory: true } destination)
        {
            return;
        }
        var path = e.DataTransfer.Items.FirstOrDefault()?.TryGetRaw(RemoteFileDrag.Format) as string;
        if (path is not null)
        {
            MoveDropped([path], destination.Path);
        }
    }

    /// Moves the dropped rows into `destination`. Mirrors FileListView.swift's
    /// move(_:to:): resolved against the search-filtered (not further
    /// sorted) rows, since sort order doesn't matter for matching by path,
    /// and a single dragged row that is itself part of the current
    /// selection takes the whole selection along with it - the same way
    /// Finder treats dragging one of several selected icons.
    private void MoveDropped(IReadOnlyList<string> droppedPaths, string destination)
    {
        var droppedIds = droppedPaths.ToHashSet();
        var searchFiltered = string.IsNullOrEmpty(_searchText)
            ? _vm.Files
            : _vm.Files.Where(f => f.Name.Contains(_searchText, StringComparison.CurrentCultureIgnoreCase));
        var sources = searchFiltered.Where(f => droppedIds.Contains(f.Id)).ToList();
        if (sources.Count == 0)
        {
            return;
        }

        if (sources.Count == 1 && _selectedIds.Contains(sources[0].Id))
        {
            var selected = _lastComputedFiles.Where(f => _selectedIds.Contains(f.Id)).ToList();
            if (selected.Count > 0)
            {
                sources = selected;
            }
        }

        var movable = sources.Where(f => RemotePath.CanMove(f.Path, destination)).ToList();
        if (movable.Count == 0)
        {
            return;
        }

        if (movable.Count < sources.Count)
        {
            var movableIds = movable.Select(f => f.Id).ToHashSet();
            foreach (var skipped in sources.Where(f => !movableIds.Contains(f.Id)))
            {
                _vm.ReportOperationError($"移動エラー ({skipped.Name}): 移動先に移動できないため対象から除外しました");
            }
        }

        _ = _vm.MoveFilesAsync(movable, destination);
    }

    // MARK: - Context menu

    private void PopulateContextMenu(ContextMenu menu, DataGridRow row)
    {
        menu.Items.Clear();
        if (row.DataContext is not RemoteFile file)
        {
            return;
        }

        // Right-clicking a row that's part of a multi-row selection acts on
        // the whole selection; an unselected row acts on just itself and
        // becomes the new selection - the same effective-selection rule
        // SwiftUI's own contextMenu(forSelectionType:) applies.
        List<RemoteFile> targets;
        if (_selectedIds.Contains(file.Id) && FilesGrid.SelectedItems.Count > 1)
        {
            targets = [.. FilesGrid.SelectedItems.Cast<RemoteFile>()];
        }
        else
        {
            targets = [file];
            FilesGrid.SelectedItems.Clear();
            FilesGrid.SelectedItems.Add(file);
        }

        if (targets.Count == 1)
        {
            var only = targets[0];
            if (only.IsDirectory)
            {
                menu.Items.Add(MenuAction("開く", () => _ = _vm.OpenFileAsync(only)));
            }
            menu.Items.Add(new Separator());
            menu.Items.Add(MenuAction("名前の変更", () => _ = RenameAsync(only)));
        }

        menu.Items.Add(MenuAction("移動", () => _ = MoveOrCopyAsync(targets, isCopy: false)));
        menu.Items.Add(MenuAction("サーバー内で複製", () => _ = MoveOrCopyAsync(targets, isCopy: true)));
        menu.Items.Add(MenuAction("パスをコピー", () => _ = CopyPathsAsync(targets)));
        menu.Items.Add(new Separator());

        var permissions = new MenuItem { Header = "権限" };
        foreach (var mode in new[] { "755", "644", "777", "600" })
        {
            permissions.Items.Add(MenuAction(mode, () => _ = _vm.ChangePermissionsAsync(targets, mode)));
        }
        menu.Items.Add(permissions);
        menu.Items.Add(new Separator());
        menu.Items.Add(MenuAction("削除", () => _ = DeleteAsync(targets), destructive: true));
    }

    private static MenuItem MenuAction(string header, Action action, bool destructive = false)
    {
        var item = new MenuItem { Header = header };
        if (destructive)
        {
            item.Foreground = Brushes.Red;
        }
        item.Click += (_, _) => action();
        return item;
    }

    // MARK: - Dialogs

    private async Task CreateFolderAsync()
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var name = await TextPromptWindow.ShowAsync(owner, "新規フォルダ", "フォルダ名", "作成");
        if (name is not null)
        {
            await _vm.CreateFolderAsync(name);
        }
    }

    private async Task RenameAsync(RemoteFile file)
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var name = await TextPromptWindow.ShowAsync(owner, "名前の変更", "新しい名前", "変更", file.Name);
        if (name is not null)
        {
            await _vm.RenameFileAsync(file, name);
        }
    }

    private async Task MoveOrCopyAsync(List<RemoteFile> targets, bool isCopy)
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var header = isCopy ? "サーバー内で複製" : "移動";
        var placeholder = isCopy ? "複製先のディレクトリ" : "移動先のディレクトリ";
        var confirmLabel = isCopy ? "複製" : "移動";
        var destination = await TextPromptWindow.ShowAsync(owner, header, placeholder, confirmLabel, _vm.CurrentPath);
        if (destination is null)
        {
            return;
        }
        if (isCopy)
        {
            await _vm.CopyFilesAsync(targets, destination);
        }
        else
        {
            await _vm.MoveFilesAsync(targets, destination);
        }
    }

    private async Task CopyPathsAsync(List<RemoteFile> targets)
    {
        if (TopLevel.GetTopLevel(this)?.Clipboard is { } clipboard)
        {
            await clipboard.SetTextAsync(string.Join("\n", targets.Select(f => f.Path)));
        }
    }

    private async Task DeleteAsync(List<RemoteFile> targets)
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var message = targets.Count == 1
            ? $"{targets[0].Name} を完全に削除します。元に戻せません。"
            : $"{targets.Count} 項目を完全に削除します。元に戻せません。";
        var confirmed = await ConfirmWindow.ShowAsync(owner, "本当に削除しますか？", message, "削除");
        if (confirmed)
        {
            await _vm.DeleteFilesAsync(targets);
        }
    }
}
