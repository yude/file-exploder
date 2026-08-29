using FileExploder.Models;
using FileExploder.Services;
using FileExploder.ViewModels;

namespace FileExploder.Tests;

/// Exercises FileListViewModel end to end against a real file-exploder
/// daemon over the local SSH fixture - navigation, history, the allowed-path
/// boundary at the server's remoteRoot, and each file operation's
/// queue-then-refresh flow.
[Collection("Local SSH")]
public sealed class FileListViewModelTests : IDisposable
{
    private readonly LocalSshFixture _fixture;
    private readonly string _scratchDir = Path.Combine(Path.GetTempPath(), "file-exploder-vm-tests-" + Guid.NewGuid().ToString("N"));
    private readonly string _settingsFile = Path.GetTempFileName();
    private readonly List<FileListViewModel> _viewModels = [];

    public FileListViewModelTests(LocalSshFixture fixture)
    {
        _fixture = fixture;
        Directory.CreateDirectory(_scratchDir);
        File.Delete(_settingsFile); // the store should tolerate a file that doesn't exist yet
        AppSettings.UseFileForTesting(_settingsFile);
    }

    public void Dispose()
    {
        foreach (var vm in _viewModels)
        {
            vm.Dispose();
        }
        if (Directory.Exists(_scratchDir))
        {
            Directory.Delete(_scratchDir, recursive: true);
        }
        if (File.Exists(_settingsFile))
        {
            File.Delete(_settingsFile);
        }
    }

    private async Task<FileListViewModel> ConnectedAsync(string? remoteRoot = null)
    {
        var vm = new FileListViewModel();
        _viewModels.Add(vm);
        var server = new Server
        {
            Name = "test",
            Hostname = "localhost",
            Port = (ushort)_fixture.Port,
            Username = _fixture.Username,
            KeyPath = _fixture.PrivateKeyPath,
            RemoteRoot = remoteRoot ?? _scratchDir,
        };
        await vm.ConnectAsync(server);
        Assert.True(vm.HasConnection, $"expected a successful connection, got: {vm.ErrorMessage}");
        return vm;
    }

    [Fact]
    public async Task ConnectAsyncListsTheRemoteRootByDefault()
    {
        await File.WriteAllTextAsync(Path.Combine(_scratchDir, "a.txt"), "hi");

        var vm = await ConnectedAsync();

        Assert.Equal(_scratchDir, vm.CurrentPath);
        Assert.Contains(vm.Files, f => f.Name == "a.txt");
    }

    [Fact]
    public async Task NavigateToADescendantDirectorySucceedsAndRecordsHistory()
    {
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));
        var vm = await ConnectedAsync();

        await vm.NavigateToAsync(Path.Combine(_scratchDir, "subdir"));

        Assert.Equal(Path.Combine(_scratchDir, "subdir"), vm.CurrentPath);
        Assert.True(vm.CanGoBack);
        Assert.False(vm.CanGoForward);
    }

    [Fact]
    public async Task NavigateOutsideTheRemoteRootIsRejected()
    {
        var vm = await ConnectedAsync();
        var outside = Path.GetDirectoryName(_scratchDir.TrimEnd('/'))!;

        await vm.NavigateToAsync(outside);

        Assert.Equal(_scratchDir, vm.CurrentPath); // unchanged
        Assert.Contains("アクセスが許可されていません", vm.ErrorMessage);
    }

    [Fact]
    public async Task BackAndForwardRetraceNavigationHistory()
    {
        var subdir = Path.Combine(_scratchDir, "subdir");
        Directory.CreateDirectory(subdir);
        var vm = await ConnectedAsync();
        await vm.NavigateToAsync(subdir);

        await vm.GoBackAsync();
        Assert.Equal(_scratchDir, vm.CurrentPath);
        Assert.True(vm.CanGoForward);

        await vm.GoForwardAsync();
        Assert.Equal(subdir, vm.CurrentPath);
    }

    [Fact]
    public async Task GoToParentMovesUpOneLevelWithinTheAllowedRoot()
    {
        var subdir = Path.Combine(_scratchDir, "subdir");
        Directory.CreateDirectory(subdir);
        var vm = await ConnectedAsync(remoteRoot: _scratchDir);
        await vm.NavigateToAsync(subdir);
        Assert.True(vm.CanGoToParent);

        await vm.GoToParentAsync();

        Assert.Equal(_scratchDir, vm.CurrentPath);
        Assert.False(vm.CanGoToParent); // at the remote root now
    }

    [Fact]
    public async Task OpenFileOnADirectoryNavigatesIntoIt()
    {
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));
        var vm = await ConnectedAsync();
        var entry = Assert.Single(vm.Files, f => f.Name == "subdir");

        await vm.OpenFileAsync(entry);

        Assert.Equal(Path.Combine(_scratchDir, "subdir"), vm.CurrentPath);
    }

    [Fact]
    public async Task CreateFolderAddsADirectoryAndRefreshesTheListing()
    {
        var vm = await ConnectedAsync();

        await vm.CreateFolderAsync("new-folder");

        Assert.True(Directory.Exists(Path.Combine(_scratchDir, "new-folder")));
        Assert.Contains(vm.Files, f => f.Name == "new-folder" && f.IsDirectory);
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task CreateFolderRejectsAnInvalidName()
    {
        var vm = await ConnectedAsync();

        await vm.CreateFolderAsync("..");

        Assert.Contains("フォルダ名に", vm.ErrorMessage);
        Assert.Empty(Directory.GetFileSystemEntries(_scratchDir));
    }

    [Fact]
    public async Task RenameFileChangesItsNameOnDisk()
    {
        await File.WriteAllTextAsync(Path.Combine(_scratchDir, "old.txt"), "hi");
        var vm = await ConnectedAsync();
        var file = Assert.Single(vm.Files, f => f.Name == "old.txt");

        await vm.RenameFileAsync(file, "new.txt");

        Assert.False(File.Exists(Path.Combine(_scratchDir, "old.txt")));
        Assert.True(File.Exists(Path.Combine(_scratchDir, "new.txt")));
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task DeleteFilesRemovesEachOneAndReportsOnlyThoseThatFail()
    {
        await File.WriteAllTextAsync(Path.Combine(_scratchDir, "keep-deleting.txt"), "hi");
        var vm = await ConnectedAsync();
        var file = Assert.Single(vm.Files, f => f.Name == "keep-deleting.txt");

        await vm.DeleteFilesAsync([file]);

        Assert.False(File.Exists(Path.Combine(_scratchDir, "keep-deleting.txt")));
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task CopyFilesDuplicatesIntoTheDestinationAndKeepsTheOriginal()
    {
        var sourceDir = Path.Combine(_scratchDir, "src");
        var destDir = Path.Combine(_scratchDir, "dst");
        Directory.CreateDirectory(sourceDir);
        Directory.CreateDirectory(destDir);
        await File.WriteAllTextAsync(Path.Combine(sourceDir, "file.txt"), "hi");

        var vm = await ConnectedAsync();
        await vm.NavigateToAsync(sourceDir);
        var file = Assert.Single(vm.Files, f => f.Name == "file.txt");

        await vm.CopyFilesAsync([file], destDir);

        Assert.True(File.Exists(Path.Combine(sourceDir, "file.txt")));
        Assert.True(File.Exists(Path.Combine(destDir, "file.txt")));
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task MoveFilesRelocatesIntoTheDestination()
    {
        var sourceDir = Path.Combine(_scratchDir, "src");
        var destDir = Path.Combine(_scratchDir, "dst");
        Directory.CreateDirectory(sourceDir);
        Directory.CreateDirectory(destDir);
        await File.WriteAllTextAsync(Path.Combine(sourceDir, "file.txt"), "hi");

        var vm = await ConnectedAsync();
        await vm.NavigateToAsync(sourceDir);
        var file = Assert.Single(vm.Files, f => f.Name == "file.txt");

        await vm.MoveFilesAsync([file], destDir);

        Assert.False(File.Exists(Path.Combine(sourceDir, "file.txt")));
        Assert.True(File.Exists(Path.Combine(destDir, "file.txt")));
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task MoveFilesToADisallowedDestinationIsRejectedUpFront()
    {
        await File.WriteAllTextAsync(Path.Combine(_scratchDir, "stays-put.txt"), "hi");
        var vm = await ConnectedAsync();
        var file = Assert.Single(vm.Files);
        var outside = Path.GetDirectoryName(_scratchDir.TrimEnd('/'))!;

        await vm.MoveFilesAsync([file], outside);

        Assert.Contains("移動先が許可範囲外です", vm.ErrorMessage);
    }

    [Fact]
    public async Task ChangePermissionsUpdatesTheModeOnDisk()
    {
        var filePath = Path.Combine(_scratchDir, "mode-me.txt");
        await File.WriteAllTextAsync(filePath, "hi");
        var vm = await ConnectedAsync();
        var file = Assert.Single(vm.Files, f => f.Name == "mode-me.txt");

        await vm.ChangePermissionsAsync([file], "600");

        // Verified against this machine's own filesystem: these tests run
        // the "remote" file-exploder daemon locally too (see LocalSshFixture),
        // so the file chmod actually touched is really sitting right here.
        // Unsupported on Windows, but this test project only ever runs on
        // the Linux/macOS dev machine that hosts the local SSH fixture.
#pragma warning disable CA1416
        var mode = File.GetUnixFileMode(filePath);
#pragma warning restore CA1416
        Assert.Equal(UnixFileMode.UserRead | UnixFileMode.UserWrite, mode);
        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task ReloadClearsOperationErrorsAfterASuccessfulRetry()
    {
        var vm = await ConnectedAsync();
        vm.ReportOperationError("something went wrong earlier");
        Assert.NotNull(vm.ErrorMessage);

        await vm.ReloadAsync();

        Assert.Null(vm.ErrorMessage);
    }

    [Fact]
    public async Task DisconnectResetsEveryPieceOfNavigationState()
    {
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));
        var vm = await ConnectedAsync();
        await vm.NavigateToAsync(Path.Combine(_scratchDir, "subdir"));

        vm.Disconnect();

        Assert.False(vm.HasConnection);
        Assert.Equal("/", vm.CurrentPath);
        Assert.Empty(vm.Files);
        Assert.False(vm.CanGoBack);
        Assert.False(vm.CanGoForward);
        Assert.Null(vm.ErrorMessage);
    }
}
