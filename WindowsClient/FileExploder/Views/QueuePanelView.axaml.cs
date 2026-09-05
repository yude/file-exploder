using Avalonia.Controls;
using Avalonia.Controls.Primitives;
using Avalonia.Data.Converters;
using Avalonia.Input.Platform;
using Avalonia.Media;
using FileExploder.Models;
using FileExploder.Services;
using FileExploder.Utilities;

namespace FileExploder.Views;

public static class QueueConverters
{
    public static readonly IValueConverter TypeToIcon = new FuncValueConverter<OperationType, string>(t => t.Icon());
    public static readonly IValueConverter TypeToDisplayName = new FuncValueConverter<OperationType, string>(t => t.DisplayName());
    public static readonly IValueConverter StatusToDisplayName = new FuncValueConverter<JobStatus, string>(s => s.DisplayName());
    public static readonly IValueConverter IsPending = new FuncValueConverter<JobStatus, bool>(s => s == JobStatus.Pending);
    public static readonly IValueConverter CompletedAtToDisplayDate =
        new FuncValueConverter<DateTimeOffset?, string?>(d => d is { } value ? FormatUtils.FormattedDate(value) : null);

    public static readonly IValueConverter StatusToBrush = new FuncValueConverter<JobStatus, IBrush>(status => status switch
    {
        JobStatus.Pending => Brushes.Orange,
        JobStatus.Running => Brushes.DodgerBlue,
        JobStatus.Completed => Brushes.Green,
        JobStatus.Failed => Brushes.Red,
        JobStatus.Cancelled => Brushes.Gray,
        _ => Brushes.Gray,
    });
}

/// Polls the queue every two seconds while a connection is live, showing
/// either the active queue or the recent-jobs history depending on which
/// tab is selected - mirrors QueuePanelView.swift's single long-lived
/// polling task shared across both tabs.
public partial class QueuePanelView : UserControl
{
    private SftpService? _sftp;
    private CancellationTokenSource? _pollCts;
    private int _selectedTab; // 0 = queue, 1 = history
    private List<QueueJob> _activeJobs = [];
    private List<QueueJob> _logJobs = [];
    private bool _isRefreshing;
    private bool _refreshRequestedAgain;

    /// The token every fetch runs under, so a connection change cancels the
    /// one in flight *and* stops its result being applied. Null between
    /// connections, when there is nothing to fetch anyway.
    private CancellationToken PollToken => _pollCts?.Token ?? CancellationToken.None;

    internal ToggleButton HistoryTabButtonForTesting => HistoryTabButton;
    internal ListBox JobListForTesting => JobList;

    public QueuePanelView()
    {
        InitializeComponent();
        QueueTabButton.IsChecked = true;
        QueueTabButton.Click += (_, _) => SelectTab(0);
        HistoryTabButton.Click += (_, _) => SelectTab(1);
        RefreshButton.Click += (_, _) =>
        {
            HideActionError();
            _ = RefreshAsync(PollToken);
        };
        CopyAllButton.Click += async (_, _) => await CopyAllLogsAsync();
        DismissActionErrorButton.Click += (_, _) => HideActionError();
        Render();
    }

    public SftpService? Sftp
    {
        get => _sftp;
        set
        {
            if (ReferenceEquals(_sftp, value))
            {
                return;
            }
            _sftp = value;
            RestartPolling();
        }
    }

    private void SelectTab(int tab)
    {
        if (_selectedTab == tab)
        {
            // A segmented control does not let its selected option
            // uncheck itself - put the pressed button's check back on.
            QueueTabButton.IsChecked = tab == 0;
            HistoryTabButton.IsChecked = tab == 1;
            return;
        }
        _selectedTab = tab;
        QueueTabButton.IsChecked = tab == 0;
        HistoryTabButton.IsChecked = tab == 1;
        Render();
        // The polling task restarts only on a connection change, not a tab
        // switch (see RestartPolling), so the just-selected tab needs its
        // own immediate fetch instead of waiting out the rest of the
        // current 2-second tick.
        _ = RefreshAsync(PollToken);
    }

    private void RestartPolling()
    {
        _pollCts?.Cancel();
        _pollCts = null;
        _activeJobs = [];
        _logJobs = [];
        HidePollError();
        Render();

        if (_sftp is null)
        {
            return;
        }
        var cts = new CancellationTokenSource();
        _pollCts = cts;
        _ = PollLoopAsync(cts.Token);
    }

    private async Task PollLoopAsync(CancellationToken cancellationToken)
    {
        while (!cancellationToken.IsCancellationRequested)
        {
            await RefreshAsync(cancellationToken);
            try
            {
                await Task.Delay(TimeSpan.FromSeconds(2), cancellationToken);
            }
            catch (OperationCanceledException)
            {
                break;
            }
        }
    }

    private async Task RefreshAsync(CancellationToken cancellationToken)
    {
        if (_sftp is null)
        {
            return;
        }
        if (_isRefreshing)
        {
            // Noted, not dropped. The fetch already in flight went out for
            // whichever tab was selected when it started, so it cannot answer
            // a request made after that - and every caller other than the poll
            // loop is exactly such a request: a tab switch (which has no data
            // at all for the tab it just moved to), the refresh button, and
            // the refresh after cancelling a job. Dropping them left the panel
            // sitting on "履歴がありません", which reads as "there is no
            // history" rather than "not fetched yet", until the next
            // two-second tick happened to land. Serve the request when the
            // in-flight fetch returns instead.
            _refreshRequestedAgain = true;
            return;
        }
        _isRefreshing = true;
        HidePollError();
        try
        {
            do
            {
                _refreshRequestedAgain = false;
                // Re-read rather than captured once: a request served by the
                // loop below may be answering a tab switched after the
                // previous pass started, and the connection can have changed
                // underneath it too.
                if (_sftp is not { } sftp)
                {
                    return;
                }
                if (_selectedTab == 0)
                {
                    var jobs = await sftp.GetQueueStatusAsync(cancellationToken);
                    if (cancellationToken.IsCancellationRequested)
                    {
                        return;
                    }
                    _activeJobs = jobs;
                }
                else
                {
                    var jobs = await sftp.GetJobLogsAsync(50, cancellationToken);
                    if (cancellationToken.IsCancellationRequested)
                    {
                        return;
                    }
                    _logJobs = jobs;
                }
                Render();
            }
            while (_refreshRequestedAgain);
        }
        catch (OperationCanceledException)
        {
            // A connection torn down mid-poll; RestartPolling already
            // cleared the displayed jobs.
        }
        catch (Exception ex)
        {
            ShowPollError(ex.Message);
        }
        finally
        {
            _isRefreshing = false;
        }
    }

    private void Render()
    {
        CopyAllButton.IsVisible = _selectedTab == 1 && _logJobs.Count > 0;

        var displayJobs = _selectedTab == 0
            ? _activeJobs.Where(j => j.Status is JobStatus.Pending or JobStatus.Running).ToList()
            : _logJobs;

        JobList.ItemsSource = displayJobs;
        JobList.IsVisible = displayJobs.Count > 0;
        EmptyState.IsVisible = displayJobs.Count == 0;
        EmptyStateIcon.Text = _selectedTab == 0 ? "📭" : "🕒";
        EmptyStateText.Text = _selectedTab == 0 ? "待機中のジョブはありません" : "履歴がありません";
    }

    private void ShowPollError(string message)
    {
        PollErrorText.Text = message;
        PollErrorText.IsVisible = true;
        JobList.IsVisible = false;
        EmptyState.IsVisible = false;
    }

    private void HidePollError()
    {
        PollErrorText.IsVisible = false;
    }

    private void ShowActionError(string message)
    {
        ActionErrorText.Text = message;
        ActionErrorBanner.IsVisible = true;
    }

    private void HideActionError() => ActionErrorBanner.IsVisible = false;

    private async void OnCancelJobClick(object? sender, Avalonia.Interactivity.RoutedEventArgs e)
    {
        if ((sender as Control)?.DataContext is not QueueJob job || _sftp is not { } sftp)
        {
            return;
        }
        try
        {
            await sftp.CancelJobAsync(job.Id);
        }
        catch (Exception ex)
        {
            ShowActionError($"キャンセルに失敗しました: {ex.Message}");
            return;
        }
        await RefreshAsync(PollToken);
    }

    private async void OnCopyLogClick(object? sender, Avalonia.Interactivity.RoutedEventArgs e)
    {
        if ((sender as Control)?.DataContext is not QueueJob job)
        {
            return;
        }
        await CopyToClipboardAsync(job.ClipboardLog);
    }

    private async Task CopyAllLogsAsync() =>
        await CopyToClipboardAsync(string.Join("\n\n---\n\n", _logJobs.Select(j => j.ClipboardLog)));

    private async Task CopyToClipboardAsync(string text)
    {
        if (TopLevel.GetTopLevel(this)?.Clipboard is { } clipboard)
        {
            await clipboard.SetTextAsync(text);
        }
    }
}
