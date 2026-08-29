using System.ComponentModel;
using Avalonia.Controls;
using FileExploder.ViewModels;

namespace FileExploder.Views;

/// The 3-pane shell: the saved-server sidebar, the connected file list (with
/// its own title bar showing the active server), and an optional queue
/// panel. Ports MainView.swift's HSplitView layout.
public partial class MainWindow : Window
{
    private readonly ConnectionViewModel _connectionVM = new();
    private readonly FileListViewModel _fileListVM = new();
    private bool _showQueuePanel;

    internal ConnectionViewModel ConnectionViewModel => _connectionVM;
    internal FileListViewModel FileListViewModel => _fileListVM;

    public MainWindow()
    {
        InitializeComponent();

        Sidebar.Attach(_connectionVM, _fileListVM);
        FileList.Attach(_fileListVM);

        _connectionVM.PropertyChanged += OnConnectionViewModelChanged;
        _fileListVM.PropertyChanged += OnFileListViewModelChanged;

        DisconnectButton.Click += (_, _) =>
        {
            _fileListVM.Disconnect();
            _connectionVM.Disconnect();
            _showQueuePanel = false;
            UpdateQueuePanel();
        };
        ToggleQueueButton.Click += (_, _) =>
        {
            _showQueuePanel = !_showQueuePanel;
            UpdateQueuePanel();
        };

        Closing += (_, _) =>
        {
            _connectionVM.Dispose();
            _fileListVM.Dispose();
        };

        UpdateTitleBar();
        UpdateQueuePanel();
    }

    private void OnConnectionViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ConnectionViewModel.ConnectedServer))
        {
            UpdateTitleBar();
        }
    }

    private void OnFileListViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(FileListViewModel.Sftp))
        {
            UpdateQueuePanel();
        }
    }

    private void UpdateTitleBar()
    {
        var server = _connectionVM.ConnectedServer;
        TitleBar.IsVisible = server is not null;
        TitleBarSeparator.IsVisible = server is not null;
        if (server is not null)
        {
            ServerNameText.Text = server.Name;
            ServerAddressText.Text = $"({server.Username}@{server.Hostname})";
        }
    }

    private void UpdateQueuePanel()
    {
        QueuePanel.IsVisible = _showQueuePanel;
        QueueSplitter.IsVisible = _showQueuePanel;
        // Only wired up to the live connection while actually shown - a
        // hidden panel has no rows on screen to keep current, so there is
        // nothing for its 2-second poll to usefully do.
        QueuePanel.Sftp = _showQueuePanel ? _fileListVM.Sftp : null;
    }
}
