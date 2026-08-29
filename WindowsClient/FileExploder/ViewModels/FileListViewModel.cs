using CommunityToolkit.Mvvm.ComponentModel;
using FileExploder.Models;
using FileExploder.Services;
using FileExploder.Utilities;

namespace FileExploder.ViewModels;

/// Owns the current directory listing and every file operation against it.
/// One instance per window, mirroring the macOS client's per-window
/// FileListViewModel over its own SSHConnection/SFTPService pair.
public sealed partial class FileListViewModel : ViewModelBase, IDisposable
{
    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(CanGoToParent))]
    public partial string CurrentPath { get; set; } = "/";

    [ObservableProperty]
    public partial IReadOnlyList<RemoteFile> Files { get; set; } = [];

    [ObservableProperty]
    public partial bool IsLoading { get; set; }

    /// Published whole so the view can tell a failed listing - which leaves
    /// nothing to show - from a failed operation, which does not.
    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(ErrorMessage))]
    public partial ErrorLog Errors { get; private set; } = new();

    public string? ErrorMessage => Errors.Message;

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(CanGoBack), nameof(CanGoForward))]
    public partial IReadOnlyList<string> PathHistory { get; set; } = [];

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(CanGoBack), nameof(CanGoForward))]
    public partial int PathHistoryIndex { get; set; } = -1;

    [ObservableProperty]
    [NotifyPropertyChangedFor(nameof(HasConnection), nameof(RemoteRoot), nameof(CanGoToParent))]
    public partial SftpService? Sftp { get; private set; }

    private SshConnection? _ssh;
    private CancellationTokenSource? _refreshCts;
    private int _navigationGeneration;
    private Action? _settingsChangedHandler;
    private Settings? _lastAppliedSettings;

    /// A queued job plus the display name it came from, so a failure that
    /// only surfaces while waiting can still say which file it was about.
    private sealed record QueuedOperation(string Id, string Name);

    private sealed record Settings(bool ShowHiddenFiles, double RefreshInterval);

    public bool HasConnection => Sftp is not null;

    public bool CanGoBack => PathHistoryIndex > 0;
    public bool CanGoForward => PathHistoryIndex < PathHistory.Count - 1;

    public string RemoteRoot => _ssh is null ? "/" : RemotePath.Standardized(_ssh.Server.RemoteRoot);

    public bool CanGoToParent
    {
        get
        {
            var current = RemotePath.Standardized(CurrentPath);
            var parent = RemotePath.Parent(current);
            return parent != current && IsPathAllowed(parent);
        }
    }

    public async Task ConnectAsync(Server server)
    {
        Disconnect();
        var sshConnection = new SshConnection(server);
        _ssh = sshConnection;
        Sftp = new SftpService(sshConnection);

        _lastAppliedSettings = CurrentSettings;
        _settingsChangedHandler = () => _ = ApplySettingsChangeIfNeededAsync();
        AppSettings.Changed += _settingsChangedHandler;

        try
        {
            await sshConnection.TestConnectionAsync();
            if (_ssh != sshConnection)
            {
                return;
            }
            await NavigateToAsync(server.RemoteRoot);
            if (ErrorMessage is { } message)
            {
                Disconnect();
                Errors = Errors.SetListingError(message);
            }
        }
        catch (Exception ex)
        {
            if (_ssh != sshConnection)
            {
                return;
            }
            var message = ex.Message;
            Disconnect();
            Errors = Errors.SetListingError(message);
        }
    }

    /// A window closed without disconnecting first must not leave its SSH
    /// process running in the background just because nothing called
    /// Disconnect() explicitly - the View calls this when the window closes.
    public void Dispose() => Disconnect();

    public void Disconnect()
    {
        _ssh?.TerminateAll();
        _refreshCts?.Cancel();
        _refreshCts = null;
        _navigationGeneration++;
        if (_settingsChangedHandler is { } handler)
        {
            AppSettings.Changed -= handler;
            _settingsChangedHandler = null;
        }

        _ssh = null;
        Sftp = null;
        _lastAppliedSettings = null;
        Files = [];
        CurrentPath = "/";
        PathHistory = [];
        PathHistoryIndex = -1;
        Errors = Errors.Clear();
        IsLoading = false;
    }

    public async Task NavigateToAsync(string path)
    {
        DismissOperationErrors();
        if (!IsPathAllowed(path))
        {
            ReportOperationError("アクセスが許可されていません");
            return;
        }
        if (await LoadPathAsync(path, updateHistory: true))
        {
            StartAutoRefresh();
        }
    }

    public async Task GoBackAsync()
    {
        DismissOperationErrors();
        if (!CanGoBack)
        {
            return;
        }
        var newIndex = PathHistoryIndex - 1;
        if (await LoadPathAsync(PathHistory[newIndex], updateHistory: false))
        {
            PathHistoryIndex = newIndex;
            StartAutoRefresh();
        }
    }

    public async Task GoForwardAsync()
    {
        DismissOperationErrors();
        if (!CanGoForward)
        {
            return;
        }
        var newIndex = PathHistoryIndex + 1;
        if (await LoadPathAsync(PathHistory[newIndex], updateHistory: false))
        {
            PathHistoryIndex = newIndex;
            StartAutoRefresh();
        }
    }

    public async Task GoToParentAsync()
    {
        var current = RemotePath.Standardized(CurrentPath);
        var parent = RemotePath.Parent(current);
        if (parent == current || !IsPathAllowed(parent))
        {
            return;
        }
        await NavigateToAsync(parent);
    }

    public async Task RefreshAsync()
    {
        if (await LoadPathAsync(CurrentPath, updateHistory: false))
        {
            StartAutoRefresh();
        }
    }

    /// The refresh the user asks for. Unlike the background and follow-up
    /// refreshes it drops the accumulated operation errors, so retrying
    /// visibly clears them when it works.
    public async Task ReloadAsync()
    {
        DismissOperationErrors();
        await RefreshAsync();
    }

    public async Task OpenFileAsync(RemoteFile file)
    {
        if (file.IsDirectory)
        {
            await NavigateToAsync(file.Path);
        }
    }

    public async Task CreateFolderAsync(string name)
    {
        if (Sftp is not { } sftp)
        {
            ReportOperationError("サーバーに接続されていません");
            return;
        }
        if (ChildPath(name) is not { } newPath)
        {
            ReportOperationError("フォルダ名に /、.、.. は使用できません");
            return;
        }
        string? finalError = null;
        try
        {
            var id = await sftp.AddToQueueAsync("mkdir", src: null, dst: newPath);
            await sftp.WaitForJobAsync(id);
        }
        catch (Exception ex)
        {
            finalError = $"フォルダ作成エラー: {ex.Message}";
        }
        await RefreshThenReportAsync(finalError is null ? [] : [finalError], sftp);
    }

    public async Task DeleteFilesAsync(IReadOnlyList<RemoteFile> files)
    {
        if (Sftp is not { } sftp)
        {
            return;
        }
        var queued = new List<QueuedOperation>();
        var finalErrors = new List<string>();
        foreach (var file in files)
        {
            if (!IsPathAllowed(file.Path))
            {
                finalErrors.Add($"削除対象が許可範囲外です: {file.Path}");
                continue;
            }
            try
            {
                var id = await sftp.AddToQueueAsync("delete", src: file.Path, dst: null);
                queued.Add(new QueuedOperation(id, file.Name));
            }
            catch (Exception ex)
            {
                finalErrors.Add($"削除登録エラー ({file.Name}): {ex.Message}");
            }
        }
        finalErrors.AddRange(await WaitForQueuedOperationsAsync(queued, sftp, "削除"));
        await RefreshThenReportAsync(finalErrors, sftp);
    }

    public async Task RenameFileAsync(RemoteFile file, string newName)
    {
        if (Sftp is not { } sftp)
        {
            return;
        }
        if (!IsPathAllowed(file.Path) || ChildPath(newName) is not { } newPath)
        {
            ReportOperationError("名前に /、.、.. は使用できません");
            return;
        }
        string? finalError = null;
        try
        {
            var id = await sftp.AddToQueueAsync("rename", src: file.Path, dst: newPath);
            await sftp.WaitForJobAsync(id);
        }
        catch (Exception ex)
        {
            finalError = $"名前変更エラー: {ex.Message}";
        }
        await RefreshThenReportAsync(finalError is null ? [] : [finalError], sftp);
    }

    public Task CopyFilesAsync(IReadOnlyList<RemoteFile> files, string destination) =>
        TransferFilesAsync(files, destination, "copy");

    public Task MoveFilesAsync(IReadOnlyList<RemoteFile> files, string destination) =>
        TransferFilesAsync(files, destination, "move");

    private async Task TransferFilesAsync(IReadOnlyList<RemoteFile> files, string destination, string type)
    {
        if (Sftp is not { } sftp)
        {
            return;
        }
        if (!IsPathAllowed(destination))
        {
            ReportOperationError(type == "copy" ? "複製先が許可範囲外です" : "移動先が許可範囲外です");
            return;
        }
        var queued = new List<QueuedOperation>();
        var finalErrors = new List<string>();
        foreach (var file in files)
        {
            if (!IsPathAllowed(file.Path))
            {
                finalErrors.Add($"対象が許可範囲外です: {file.Path}");
                continue;
            }
            var destinationPath = RemotePath.Appending(file.Name, destination);
            try
            {
                var id = await sftp.AddToQueueAsync(type, src: file.Path, dst: destinationPath);
                queued.Add(new QueuedOperation(id, file.Name));
            }
            catch (Exception ex)
            {
                finalErrors.Add($"登録エラー ({file.Name}): {ex.Message}");
            }
        }
        var actionName = type == "copy" ? "コピー" : "移動";
        finalErrors.AddRange(await WaitForQueuedOperationsAsync(queued, sftp, actionName));
        await RefreshThenReportAsync(finalErrors, sftp);
    }

    public async Task ChangePermissionsAsync(IReadOnlyList<RemoteFile> files, string mode)
    {
        if (Sftp is not { } sftp)
        {
            return;
        }
        var queued = new List<QueuedOperation>();
        var finalErrors = new List<string>();
        foreach (var file in files)
        {
            if (!IsPathAllowed(file.Path))
            {
                finalErrors.Add($"権限変更対象が許可範囲外です: {file.Path}");
                continue;
            }
            try
            {
                var id = await sftp.AddToQueueAsync("chmod", src: null, dst: file.Path, mode: mode);
                queued.Add(new QueuedOperation(id, file.Name));
            }
            catch (Exception ex)
            {
                finalErrors.Add($"権限変更登録エラー ({file.Name}): {ex.Message}");
            }
        }
        finalErrors.AddRange(await WaitForQueuedOperationsAsync(queued, sftp, "権限変更"));
        await RefreshThenReportAsync(finalErrors, sftp);
    }

    /// The toolbar disables itself while IsLoading, but breadcrumb
    /// navigation and double-click-to-open don't go through the toolbar at
    /// all - each fires independently regardless of what's already in
    /// flight. Guarding here, in the one place every navigation path funnels
    /// through, covers all of them at once instead of needing the same
    /// IsLoading check duplicated at every UI entry point: whichever call is
    /// already running keeps going, and a call that arrives while it's in
    /// flight is simply dropped rather than starting a second concurrent
    /// ListDirectoryAsync against the same connection.
    private async Task<bool> LoadPathAsync(string path, bool updateHistory)
    {
        if (IsLoading)
        {
            return false;
        }
        if (Sftp is not { } sftp || !IsPathAllowed(path))
        {
            if (Sftp is not null)
            {
                ReportOperationError("アクセスが許可されていません");
            }
            return false;
        }

        _navigationGeneration++;
        var currentGeneration = _navigationGeneration;
        IsLoading = true;
        Errors = Errors.SetListingError(null);

        List<RemoteFile> fileList;
        try
        {
            fileList = await sftp.ListDirectoryAsync(path);
        }
        catch (Exception ex)
        {
            if (currentGeneration != _navigationGeneration)
            {
                return false;
            }
            Errors = Errors.SetListingError(ex.Message);
            IsLoading = false;
            return false;
        }
        if (currentGeneration != _navigationGeneration)
        {
            return false;
        }

        if (updateHistory)
        {
            var history = PathHistoryIndex < PathHistory.Count - 1
                ? PathHistory.Take(PathHistoryIndex + 1)
                : PathHistory;
            PathHistory = [.. history, path];
            PathHistoryIndex = PathHistory.Count - 1;
        }

        CurrentPath = path;
        Files = SortedVisibleFiles(fileList);
        IsLoading = false;
        return true;
    }

    private static Settings CurrentSettings => new(AppSettings.ShowHiddenFiles, AppSettings.RefreshInterval);

    /// AppSettings.Changed fires for every settings write, the saved server
    /// list included (SavedServersStore has its own event, but a future
    /// setting added alongside these two should not need to remember to
    /// re-check this guard). Re-listing only when a setting this view model
    /// actually reads has changed keeps unrelated writes from triggering an
    /// SSH round trip.
    private async Task ApplySettingsChangeIfNeededAsync()
    {
        var settings = CurrentSettings;
        if (_lastAppliedSettings is not { } previous)
        {
            _lastAppliedSettings = settings;
            return;
        }
        if (settings == previous)
        {
            return;
        }
        _lastAppliedSettings = settings;

        // Only the hidden-file setting changes which entries belong on
        // screen, and only a fresh listing can supply the ones being
        // revealed. The refresh interval just re-arms the timer -
        // re-listing for it meant dragging the slider fired one SSH round
        // trip per step of the drag.
        if (settings.ShowHiddenFiles != previous.ShowHiddenFiles)
        {
            await RefreshAsync();
        }
        else
        {
            StartAutoRefresh();
        }
    }

    /// Consecutive background-refresh failures tolerated before surfacing an
    /// error, mirroring SftpService's own poll-failure tolerance - one
    /// dropped connection must not flash an error the very next tick clears
    /// on its own.
    private const int AutoRefreshFailuresTolerated = 3;

    private void StartAutoRefresh()
    {
        _refreshCts?.Cancel();
        _refreshCts = null;

        if (Sftp is null)
        {
            return;
        }
        var interval = AppSettings.RefreshInterval;
        if (!double.IsFinite(interval) || interval <= 0)
        {
            return;
        }
        interval = Math.Min(interval, 300);
        var startGeneration = _navigationGeneration;

        var cts = new CancellationTokenSource();
        _refreshCts = cts;
        _ = RunAutoRefreshLoopAsync(TimeSpan.FromSeconds(interval), startGeneration, cts.Token);
    }

    private async Task RunAutoRefreshLoopAsync(TimeSpan interval, int startGeneration, CancellationToken cancellationToken)
    {
        var consecutiveFailures = 0;
        var hasReportedFailure = false;
        while (!cancellationToken.IsCancellationRequested)
        {
            try
            {
                await Task.Delay(interval, cancellationToken);
            }
            catch (OperationCanceledException)
            {
                break;
            }
            if (cancellationToken.IsCancellationRequested || _navigationGeneration != startGeneration)
            {
                break;
            }
            if (Sftp is not { } sftp)
            {
                continue; // not connected (yet/anymore); keep waiting
            }

            try
            {
                var fileList = await sftp.ListDirectoryAsync(CurrentPath, cancellationToken);
                if (cancellationToken.IsCancellationRequested || _navigationGeneration != startGeneration)
                {
                    break;
                }
                Files = SortedVisibleFiles(fileList);
                consecutiveFailures = 0;
                hasReportedFailure = false;
            }
            catch (OperationCanceledException)
            {
                break;
            }
            catch (Exception ex)
            {
                if (cancellationToken.IsCancellationRequested || _navigationGeneration != startGeneration)
                {
                    break;
                }
                // A background refresh used to swallow every failure and
                // retry forever with nothing to show for it - a listing
                // that quietly stopped updating looked identical to one
                // that hadn't changed. Surface it once it's persistent
                // rather than a single blip, through the dismissible
                // operation-error banner, not the listing error: that slot
                // replaces the whole file table with a full-page retry
                // screen, which would hide an already-loaded, still-valid
                // listing behind an error about a refresh that merely
                // couldn't confirm it's still current. Reported at most once
                // per failing streak, not on every poll past the threshold.
                consecutiveFailures++;
                if (consecutiveFailures > AutoRefreshFailuresTolerated && !hasReportedFailure)
                {
                    hasReportedFailure = true;
                    ReportOperationError($"バックグラウンド更新エラー: {ex.Message}");
                }
            }
        }
    }

    private static List<RemoteFile> SortedVisibleFiles(List<RemoteFile> fileList)
    {
        var visible = AppSettings.ShowHiddenFiles
            ? fileList
            : fileList.Where(f => !f.Name.StartsWith('.')).ToList();
        return [.. visible
            .OrderBy(f => !f.IsDirectory)
            .ThenBy(f => f.Name, StringComparer.CurrentCultureIgnoreCase)];
    }

    private bool IsPathAllowed(string path) => _ssh is not null && RemotePath.IsDescendant(path, RemoteRoot);

    private string? ChildPath(string name)
    {
        if (!RemotePath.IsValidComponent(name))
        {
            return null;
        }
        var path = RemotePath.Appending(name, CurrentPath);
        return IsPathAllowed(path) ? path : null;
    }

    /// Waits for every queued operation and turns each unsuccessful outcome
    /// into the same "<action>エラー (<name>): <message>" format a per-file
    /// loop calling WaitForJobAsync once per file used to build one entry at
    /// a time - just backed by SftpService.WaitForJobsAsync's single shared
    /// poll instead.
    private static async Task<List<string>> WaitForQueuedOperationsAsync(List<QueuedOperation> queued, SftpService sftp, string actionName)
    {
        if (queued.Count == 0)
        {
            return [];
        }
        try
        {
            var failures = await sftp.WaitForJobsAsync([.. queued.Select(q => q.Id)]);
            return [.. queued
                .Where(operation => failures.ContainsKey(operation.Id))
                .Select(operation => $"{actionName}エラー ({operation.Name}): {failures[operation.Id]}")];
        }
        catch (Exception ex)
        {
            return [.. queued.Select(operation => $"{actionName}エラー ({operation.Name}): {ex.Message}")];
        }
    }

    /// Reports what an operation ran into, but only while the session it ran
    /// against is still the current one. Switching servers - or windows -
    /// mid-operation would otherwise surface errors about the old host next
    /// to the new host's file list, after Disconnect() had already retired
    /// them.
    private async Task RefreshThenReportAsync(IReadOnlyList<string> newErrors, SftpService service)
    {
        if (!ReferenceEquals(service, Sftp))
        {
            return;
        }
        Errors = Errors.AddOperationErrors(newErrors);
        await RefreshAsync();
    }

    /// Not private: the view reports a few things it declines on its own -
    /// a drag with one entry it won't move alongside others it will - that
    /// never reach a ViewModel method at all.
    public void ReportOperationError(string message)
    {
        Errors = Errors.AddOperationError(message);
    }

    /// Retires the operation errors. Called when the user navigates,
    /// reloads, or dismisses the banner - never by a background refresh,
    /// which would put back the disappearing-error bug these are separated
    /// to avoid.
    public void DismissOperationErrors()
    {
        Errors = Errors.ClearOperationErrors();
    }
}
