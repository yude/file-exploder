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

    private async void OnEditServerClick(object? sender, Avalonia.Interactivity.RoutedEventArgs e)
    {
        if (RowFor(sender) is { } row)
        {
            await OpenEditorAsync(row.Server);
        }
    }

    private async void OnDeleteServerClick(object? sender, Avalonia.Interactivity.RoutedEventArgs e)
    {
        if (RowFor(sender) is not { } row || TopLevel.GetTopLevel(this) is not Window owner)
        {
            return;
        }
        var server = row.Server;
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

    /// MenuItem.Click bubbles from the context menu, which is attached per
    /// ListBoxItem via a style setter (see the .axaml) - DataContext at that
    /// point is the ServerRowItem the menu was opened on.
    private static ServerRowItem? RowFor(object? sender) =>
        (sender as Control)?.DataContext as ServerRowItem;
}
