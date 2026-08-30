using System.ComponentModel;
using Avalonia.Controls;
using Avalonia.Data.Converters;
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
        ServerList.SelectionChanged += async (_, e) =>
        {
            if (e.AddedItems.Count > 0 && e.AddedItems[0] is ServerRowItem { Server: var server })
            {
                await _connectionVM.ConnectAsync(server, _fileListVM);
            }
        };

        // Built per-container, not via a shared <Style> Setter: a Setter's
        // value is one XAML-parsed instance reused for every element the
        // style matches, and a ContextMenu (a Control, so it can only ever
        // have one parent) silently fails to reopen once RefreshRows()
        // rebuilds the list's containers - which happens on every
        // Servers/ConnectedServer change, connecting included. That is
        // exactly the shape of "right-click stops working once connected"
        // this replaces.
        ServerList.ContainerPrepared += OnContainerPrepared;
        ServerList.ContainerClearing += OnContainerClearing;
    }

    private void OnContainerPrepared(object? sender, ContainerPreparedEventArgs e)
    {
        var container = e.Container;
        var menu = new ContextMenu();
        menu.Opening += (_, _) => PopulateContextMenu(menu, container);
        container.ContextMenu = menu;
    }

    private void OnContainerClearing(object? sender, ContainerClearingEventArgs e) =>
        e.Container.ContextMenu = null;

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
