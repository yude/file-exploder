using System.ComponentModel;
using Avalonia.Controls;
using Avalonia.Input;
using CommunityToolkit.Mvvm.Input;
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
    internal FileListView FileListViewForTesting => FileList;
    internal SidebarView SidebarForTesting => Sidebar;
    internal QueuePanelView QueuePanelForTesting => QueuePanel;

    // The generated fields for these two menu items are otherwise private,
    // and (unlike the always-visible menu bar itself) their containing
    // Popup may not be realized in the visual tree until actually opened -
    // exposing them directly is simpler and more robust for tests than
    // relying on visual-tree traversal into a collapsed submenu.
    internal MenuItem NewWindowMenuItemForTesting => NewWindowMenuItem;
    internal MenuItem SettingsMenuItemForTesting => SettingsMenuItem;

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

        NewWindowMenuItem.Click += (_, _) => OpenNewWindow();
        SettingsMenuItem.Click += (_, _) => OpenSettings();
        KeyBindings.Add(new KeyBinding
        {
            Gesture = new KeyGesture(Key.N, KeyModifiers.Control),
            Command = new RelayCommand(() => OpenNewWindow()),
        });
        KeyBindings.Add(new KeyBinding
        {
            Gesture = new KeyGesture(Key.OemComma, KeyModifiers.Control),
            Command = new RelayCommand(OpenSettings),
        });

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

    /// Ports FileExploderWindowCommands' "新しいウィンドウ" (Cmd+N on macOS):
    /// each window owns its own FileListViewModel/ConnectionViewModel, but
    /// they all share the same saved-server list and settings via
    /// SavedServersStore/AppSettings, exactly like opening a second
    /// WindowGroup(id: "main") instance does.
    /// Lets a test observe (and close) a window opened indirectly, through
    /// the menu item or key binding, instead of only ones it opened itself
    /// via OpenNewWindowForTesting - an unclosed window's ConnectionViewModel
    /// stays subscribed to SavedServersStore's process-wide Changed event
    /// for the rest of the test run otherwise, and can then fail an
    /// unrelated later test on a different thread.
    internal static event Action<MainWindow>? WindowOpenedForTesting;

    private static MainWindow OpenNewWindow()
    {
        var window = new MainWindow();
        window.Show();
        WindowOpenedForTesting?.Invoke(window);
        return window;
    }

    /// Ports FileExploderApp.swift's Settings scene (macOS-only there; a
    /// plain window here, since Windows has no equivalent scene concept).
    private void OpenSettings() => new SettingsWindow().Show(this);
}
