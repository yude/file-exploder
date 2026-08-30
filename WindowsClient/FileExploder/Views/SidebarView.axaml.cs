using System.ComponentModel;
using Avalonia.Controls;
using Avalonia.Data.Converters;
using Avalonia.Input;
using Avalonia.Interactivity;
using Avalonia.Media;
using FileExploder.Models;
using FileExploder.ViewModels;

namespace FileExploder.Views;

/// One row of the server list, pairing a saved server with whether it is
/// the one currently connected - computed here rather than carried on
/// Server itself, the same way the macOS client's SidebarView recomputes
/// isConnected for each ServerRow from connectionVM.connectedServer on every
/// render.
public sealed record ServerRowItem(Server Server, bool IsConnected);

public static class Converters
{
    public static readonly IValueConverter BoolToConnectedBrush =
        new FuncValueConverter<bool, IBrush>(connected => connected ? Brushes.Green : Brushes.Gray);
}

public partial class SidebarView : UserControl
{
    private ConnectionViewModel _connectionVM = null!;
    private FileListViewModel _fileListVM = null!;

    internal ListBox ServerListForTesting => ServerList;

    public SidebarView()
    {
        InitializeComponent();
        AddButton.Click += async (_, _) => await OpenEditorAsync(null);

        // Connecting is driven by an explicit left-click on a row, NOT by
        // ListBox.SelectionChanged. Selection changes for reasons that are
        // not "the user asked to connect to this server": a right-click
        // selects the row before opening its context menu, and replacing
        // ItemsSource (which RefreshRows does on every Servers/
        // ConnectedServer change) raises it too. Driving ConnectAsync from
        // selection meant right-clicking a row kicked off a connection,
        // whose own IsConnecting/ConnectedServer updates immediately
        // rebuilt the list underneath the context menu that was trying to
        // open - so the menu never appeared, and editing a server became
        // impossible. Verified by direct repro before and after this change.
        ServerList.ContainerPrepared += OnContainerPrepared;
        ServerList.ContainerClearing += OnContainerClearing;
    }

    private void OnContainerPrepared(object? sender, ContainerPreparedEventArgs e)
    {
        var container = e.Container;
        var menu = new ContextMenu();
        menu.Opening += (_, _) => PopulateContextMenu(menu, container);
        container.ContextMenu = menu;
        container.AddHandler(PointerReleasedEvent, OnRowPointerReleased, RoutingStrategies.Tunnel);
    }

    private void OnRowPointerReleased(object? sender, PointerReleasedEventArgs e)
    {
        if (e.InitialPressMouseButton != MouseButton.Left)
        {
            return;
        }
        if ((sender as Control)?.DataContext is ServerRowItem row)
        {
            _ = _connectionVM.ConnectAsync(row.Server, _fileListVM);
        }
    }

    private void OnContainerClearing(object? sender, ContainerClearingEventArgs e)
    {
        e.Container.ContextMenu = null;
        e.Container.RemoveHandler(PointerReleasedEvent, OnRowPointerReleased);
    }

    internal void PopulateContextMenuForTesting(ContextMenu menu, Control container) => PopulateContextMenu(menu, container);

    private void PopulateContextMenu(ContextMenu menu, Control container)
    {
        menu.Items.Clear();
        if (container.DataContext is not ServerRowItem row)
        {
            return;
        }
        menu.Items.Add(MenuAction("編集", () => _ = OpenEditorAsync(row.Server)));
        menu.Items.Add(new Separator());
        menu.Items.Add(MenuAction("削除", () => _ = DeleteServerAsync(row.Server)));
    }

    private static MenuItem MenuAction(string header, Action action)
    {
        var item = new MenuItem { Header = header };
        item.Click += (_, _) => action();
        return item;
    }

    public void Attach(ConnectionViewModel connectionVM, FileListViewModel fileListVM)
    {
        _connectionVM = connectionVM;
        _fileListVM = fileListVM;
        DataContext = connectionVM;
        connectionVM.PropertyChanged += OnConnectionViewModelChanged;
        RefreshRows();
    }

    private void OnConnectionViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName is nameof(ConnectionViewModel.Servers) or nameof(ConnectionViewModel.ConnectedServer))
        {
            RefreshRows();
        }
    }

    private void RefreshRows()
    {
        var connectedId = _connectionVM.ConnectedServer?.Id;
        ServerList.ItemsSource = _connectionVM.Servers
            .Select(server => new ServerRowItem(server, server.Id == connectedId))
            .ToList();
    }

    private async Task OpenEditorAsync(Server? server)
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        await ConnectionWindow.ShowAsync(owner, _connectionVM, _fileListVM, server);
    }

    private async Task DeleteServerAsync(Server server)
    {
        if (TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var confirmed = await ConfirmWindow.ShowAsync(
            owner,
            "サーバー設定を削除しますか？",
            $"{server.Name} ({server.Username}@{server.Hostname}) の接続設定を削除します。サーバー上のファイルは削除されません。",
            "削除");
        if (!confirmed)
        {
            return;
        }
        if (_connectionVM.ActiveServerId == server.Id)
        {
            _fileListVM.Disconnect();
        }
        _connectionVM.DeleteServer(server);
    }
}
