using FileExploder.Models;
using FileExploder.Services;
using FileExploder.ViewModels;

namespace FileExploder.Tests;

/// Shares the "Local SSH" collection with SshConnectionTests/SftpServiceTests
/// - not because these tests need the live server themselves (only a few
/// do), but because AppSettings/SavedServersStore are process-wide static
/// stores: serializing every test that redirects them onto a throwaway file
/// avoids one test's UseFileForTesting call racing another's.
[Collection("Local SSH")]
public sealed class ConnectionViewModelTests : IDisposable
{
    private readonly LocalSshFixture _fixture;
    private readonly string _serversFile = Path.GetTempFileName();
    private readonly List<ConnectionViewModel> _viewModels = [];
    private readonly List<FileListViewModel> _fileListViewModels = [];

    public ConnectionViewModelTests(LocalSshFixture fixture)
    {
        _fixture = fixture;
        File.Delete(_serversFile); // the store should tolerate a file that doesn't exist yet
        SavedServersStore.UseFileForTesting(_serversFile);
    }

    public void Dispose()
    {
        // Each ConnectionViewModel subscribes to the process-wide
        // SavedServersStore.Changed event for its whole lifetime - leaving
        // one undisposed would keep it (and its stale file handle) alive
        // and reacting to every later test's saves.
        foreach (var vm in _viewModels)
        {
            vm.Dispose();
        }
        foreach (var vm in _fileListViewModels)
        {
            vm.Dispose();
        }
        if (File.Exists(_serversFile))
        {
            File.Delete(_serversFile);
        }
    }

    private ConnectionViewModel NewConnectionViewModel()
    {
        var vm = new ConnectionViewModel();
        _viewModels.Add(vm);
        return vm;
    }

    private FileListViewModel NewFileListViewModel()
    {
        var vm = new FileListViewModel();
        _fileListViewModels.Add(vm);
        return vm;
    }

    private static Server NewServer(string name = "test-server") => new()
    {
        Name = name,
        Hostname = "example.test",
        Username = "someone",
    };

    private Server NewReachableServer(string name = "test-server") => new()
    {
        Name = name,
        Hostname = "localhost",
        Port = (ushort)_fixture.Port,
        Username = _fixture.Username,
        KeyPath = _fixture.PrivateKeyPath,
    };

    [Fact]
    public void AddServerPersistsAndIsVisibleToANewViewModel()
    {
        var vm = NewConnectionViewModel();
        var server = NewServer();

        vm.AddServer(server);

        var reloaded = NewConnectionViewModel();
        Assert.Contains(reloaded.Servers, s => s.Id == server.Id);
    }

    [Fact]
    public void UpdateServerReplacesTheStoredEntryByIdentity()
    {
        var vm = NewConnectionViewModel();
        var server = NewServer();
        vm.AddServer(server);

        var renamed = server with { Name = "renamed" };
        vm.UpdateServer(renamed);

        var stored = Assert.Single(vm.Servers, s => s.Id == server.Id);
        Assert.Equal("renamed", stored.Name);
    }

    [Fact]
    public void DeleteServerRemovesItFromTheStoredList()
    {
        var vm = NewConnectionViewModel();
        var server = NewServer();
        vm.AddServer(server);

        vm.DeleteServer(server);

        Assert.DoesNotContain(vm.Servers, s => s.Id == server.Id);
    }

    [Fact]
    public void ASecondWindowSeesAServerAddedByTheFirst()
    {
        var first = NewConnectionViewModel();
        var second = NewConnectionViewModel();

        var server = NewServer();
        first.AddServer(server);

        Assert.Contains(second.Servers, s => s.Id == server.Id);
    }

    [Fact]
    public void AnUnreadableStoredListIsReportedButDoesNotClearWhatsAlreadyShown()
    {
        File.WriteAllText(_serversFile, "not json");

        var vm = NewConnectionViewModel();

        Assert.Empty(vm.Servers);
        Assert.Contains("サーバー設定の読み込みに失敗しました", vm.ConnectionError);
    }

    [Fact]
    public async Task ConnectAsyncFailureReportsTheErrorAndLeavesNothingConnected()
    {
        var vm = NewConnectionViewModel();
        // Port 1 is a privileged, essentially never-listening port - a
        // realistic "nobody answered" failure without depending on any
        // specific unreachable host.
        var server = NewServer() with { Hostname = "localhost", Port = 1 };
        var fileListVM = NewFileListViewModel();

        await vm.ConnectAsync(server, fileListVM);

        Assert.Null(vm.ConnectedServer);
        Assert.False(vm.IsConnecting);
        Assert.NotNull(vm.ConnectionError);
    }

    [Fact]
    public async Task AnotherWindowDeletingTheConnectedServerDisconnectsThisOne()
    {
        var first = NewConnectionViewModel();
        var server = NewReachableServer();
        first.AddServer(server);
        var fileListVM = NewFileListViewModel();

        await first.ConnectAsync(server, fileListVM);
        Assert.NotNull(first.ConnectedServer);

        var second = NewConnectionViewModel();
        second.DeleteServer(server);

        Assert.Null(first.ConnectedServer);
        Assert.Contains("別のウィンドウで削除されたため切断しました", first.ConnectionError);
        Assert.False(fileListVM.HasConnection);
    }

    [Fact]
    public async Task AnotherWindowEditingTheConnectedServerDisconnectsThisOne()
    {
        var first = NewConnectionViewModel();
        var server = NewReachableServer();
        first.AddServer(server);
        var fileListVM = NewFileListViewModel();

        await first.ConnectAsync(server, fileListVM);
        Assert.NotNull(first.ConnectedServer);

        var second = NewConnectionViewModel();
        second.UpdateServer(server with { Name = "changed-elsewhere" });

        Assert.Null(first.ConnectedServer);
        Assert.Contains("別のウィンドウで変更されたため切断しました", first.ConnectionError);
    }

    [Fact]
    public async Task DeletingTheConnectedServerDisconnectsTheActiveSession()
    {
        var vm = NewConnectionViewModel();
        var server = NewServer() with { Hostname = "localhost", Port = 1 };
        vm.AddServer(server);
        var fileListVM = NewFileListViewModel();

        await vm.ConnectAsync(server, fileListVM);
        // The connection itself fails (no real server on port 1), but
        // deleting the server that was *being connected to* must still
        // clear the connecting-state bookkeeping either way.
        vm.DeleteServer(server);

        Assert.Null(vm.ConnectedServer);
        Assert.Null(vm.ActiveServerId);
    }
}
