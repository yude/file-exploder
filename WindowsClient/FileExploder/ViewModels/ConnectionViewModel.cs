using System.Text.Json;
using CommunityToolkit.Mvvm.ComponentModel;
using FileExploder.Models;
using FileExploder.Services;

namespace FileExploder.ViewModels;

/// Owns the saved-server list and which one (if any) this window is
/// connected to. Every open window's ConnectionViewModel shares the same
/// persisted list via SavedServersStore - the equivalent of every macOS
/// client window's ConnectionViewModel reading the same UserDefaults key.
public sealed partial class ConnectionViewModel : ViewModelBase, IDisposable
{
    [ObservableProperty]
    public partial IReadOnlyList<Server> Servers { get; set; } = [];

    [ObservableProperty]
    public partial Server? ConnectedServer { get; set; }

    [ObservableProperty]
    public partial bool IsConnecting { get; set; }

    [ObservableProperty]
    public partial string? ConnectionError { get; set; }

    private int _connectionGeneration;
    private Guid? _connectingServerId;
    private FileListViewModel? _activeFileListViewModel;
    private Server? _activeServerSnapshot;

    public Guid? ActiveServerId => ConnectedServer?.Id ?? _connectingServerId;

    public ConnectionViewModel()
    {
        LoadServers();
        // Cmd+N opens a second window, and each one builds its own view
        // model over the same saved list. Without this, a window would keep
        // showing the list exactly as it looked when it opened.
        SavedServersStore.Changed += ReloadIfChanged;
    }

    public void Dispose()
    {
        SavedServersStore.Changed -= ReloadIfChanged;
    }

    public void LoadServers()
    {
        if (DecodeStoredServers() is { } stored)
        {
            Servers = stored;
        }
    }

    /// Returns the persisted list, or null when it is present but unreadable
    /// - in which case the caller keeps whatever it already has rather than
    /// treating a decode failure as "the user has no servers".
    ///
    /// SavedServersStore.Load already drops individual entries this version
    /// doesn't recognize (see LenientJson) instead of failing the whole
    /// list, so a JsonException here means the file itself is unreadable,
    /// not merely that one entry is in an unrecognized shape.
    private List<Server>? DecodeStoredServers()
    {
        try
        {
            return SavedServersStore.Load();
        }
        catch (JsonException ex)
        {
            ConnectionError = $"サーバー設定の読み込みに失敗しました: {ex.Message}";
            return null;
        }
    }

    private void ReloadIfChanged()
    {
        if (DecodeStoredServers() is not { } stored || stored.SequenceEqual(Servers))
        {
            return;
        }
        Servers = stored;

        // A different window may edit or delete the server this window is
        // using. Keeping the old SSH session alive while showing the new
        // saved details is misleading and can send operations to the wrong
        // host.
        if (_activeServerSnapshot is { } active)
        {
            var stillMatches = stored.FirstOrDefault(s => s.Id == active.Id) == active;
            if (!stillMatches)
            {
                var reason = stored.Any(s => s.Id == active.Id)
                    ? "接続中のサーバー設定が別のウィンドウで変更されたため切断しました"
                    : "接続中のサーバーが別のウィンドウで削除されたため切断しました";
                DisconnectActiveSession(reason);
            }
        }
    }

    /// Applies a change to the list as it is stored *right now*. Writing
    /// back a copy this window loaded earlier would drop whatever another
    /// window saved in between: add a server in one window, then one in a
    /// second, and the first disappears.
    private void MutateServers(Action<List<Server>> mutate)
    {
        var list = DecodeStoredServers() ?? [.. Servers];
        mutate(list);
        Servers = list;
        try
        {
            SavedServersStore.Save(list);
        }
        catch (IOException ex)
        {
            ConnectionError = $"サーバー設定の保存に失敗しました: {ex.Message}";
        }
    }

    public void AddServer(Server server) => MutateServers(list => list.Add(server));

    public void UpdateServer(Server server) => MutateServers(list =>
    {
        var index = list.FindIndex(s => s.Id == server.Id);
        if (index >= 0)
        {
            list[index] = server;
        }
    });

    public void DeleteServer(Server server)
    {
        MutateServers(list => list.RemoveAll(s => s.Id == server.Id));
        if (ConnectedServer?.Id == server.Id || _connectingServerId == server.Id)
        {
            DisconnectActiveSession();
        }
    }

    public async Task ConnectAsync(Server server, FileListViewModel fileListViewModel)
    {
        _connectionGeneration++;
        var generation = _connectionGeneration;
        _connectingServerId = server.Id;
        IsConnecting = true;
        ConnectedServer = null;
        ConnectionError = null;
        _activeFileListViewModel = fileListViewModel;
        _activeServerSnapshot = server;

        await fileListViewModel.ConnectAsync(server);
        if (generation != _connectionGeneration)
        {
            return;
        }

        if (fileListViewModel.ErrorMessage is null)
        {
            ConnectedServer = server;
        }
        else
        {
            ConnectionError = fileListViewModel.ErrorMessage;
            _activeFileListViewModel = null;
            _activeServerSnapshot = null;
        }

        _connectingServerId = null;
        IsConnecting = false;
    }

    public void Disconnect()
    {
        _connectionGeneration++;
        _connectingServerId = null;
        IsConnecting = false;
        ConnectedServer = null;
        _activeFileListViewModel = null;
        _activeServerSnapshot = null;
    }

    /// `reason` is surfaced when the session ended on its own - a deliberate
    /// delete already speaks for itself and would only add noise.
    private void DisconnectActiveSession(string? reason = null)
    {
        _activeFileListViewModel?.Disconnect();
        Disconnect();
        if (reason is not null)
        {
            ConnectionError = reason;
        }
    }
}
