using Avalonia.Controls;
using Avalonia.Threading;
using Avalonia.VisualTree;
using FileExploder.Models;
using FileExploder.Services;
using FileExploder.Views;

namespace FileExploder.Tests;

/// A ListBoxItem's ContextMenu, when set via a shared <Style> Setter (as
/// this used to be), is one XAML-parsed instance reused across every
/// matching container - verified directly: a 3-item ListBox styled that way
/// reports the exact same ContextMenu reference on all three containers.
/// Since a ContextMenu is a Control and can only ever have one owner, this
/// broke down exactly when SidebarView.RefreshRows() rebuilds the server
/// list's containers, which happens on every Servers/ConnectedServer change
/// - connecting included. These tests exercise the per-container
/// ContextMenu (built in ContainerPrepared/ContainerClearing) that replaced
/// it.
[Collection("Local SSH")]
public sealed class SidebarViewTests : IDisposable
{
    private readonly LocalSshFixture _fixture;
    private readonly string _serversFile = Path.GetTempFileName();

    public SidebarViewTests(LocalSshFixture fixture)
    {
        _fixture = fixture;
        File.Delete(_serversFile); // the store should tolerate a file that doesn't exist yet
        SavedServersStore.UseFileForTesting(_serversFile);
    }

    public void Dispose()
    {
        if (File.Exists(_serversFile))
        {
            File.Delete(_serversFile);
        }
    }

    [Fact]
    public void EachServerRowKeepsItsOwnContextMenuAfterConnecting() => HeadlessApp.RunOnUiThread(() =>
    {
        var window = new MainWindow();
        window.Show();
        Dispatcher.UIThread.RunJobs();

        var serverA = new Server { Name = "a", Hostname = "example.test", Username = "someone" };
        var serverB = new Server
        {
            Name = "b",
            Hostname = "localhost",
            Port = (ushort)_fixture.Port,
            Username = _fixture.Username,
            KeyPath = _fixture.PrivateKeyPath,
        };
        window.ConnectionViewModel.AddServer(serverA);
        window.ConnectionViewModel.AddServer(serverB);
        Dispatcher.UIThread.RunJobs();

        var listBox = window.SidebarForTesting.ServerListForTesting;
        AssertEveryRowHasItsOwnContextMenu(listBox, expectedRows: 2);

        // Connecting sets ConnectedServer, which is exactly what
        // RefreshRows() reacts to by rebuilding ItemsSource - the same
        // rebuild that broke right-click with the old shared ContextMenu.
        TestUiHelpers.PumpUntilCompleted(window.ConnectionViewModel.ConnectAsync(serverB, window.FileListViewModel), TimeSpan.FromSeconds(15));
        Dispatcher.UIThread.RunJobs();

        AssertEveryRowHasItsOwnContextMenu(listBox, expectedRows: 2);

        window.Close();
    });

    /// The context menu is rebuilt fresh (in ContainerPrepared) each time a
    /// container is recycled, so it must reflect whichever server is
    /// *currently* bound to that container, not whichever server it was
    /// built for originally.
    [Fact]
    public void ContextMenuActsOnTheServerCurrentlyBoundToItsRow() => HeadlessApp.RunOnUiThread(() =>
    {
        var window = new MainWindow();
        window.Show();
        Dispatcher.UIThread.RunJobs();

        var serverA = new Server { Name = "a", Hostname = "example.test", Username = "someone" };
        window.ConnectionViewModel.AddServer(serverA);
        Dispatcher.UIThread.RunJobs();

        var listBox = window.SidebarForTesting.ServerListForTesting;
        var container = listBox.GetVisualDescendants().OfType<ListBoxItem>().Single();
        var menu = container.ContextMenu!;

        // Renaming rebuilds the Servers list (a new ServerRowItem instance,
        // same underlying id) - the row's container is recycled, not
        // replaced, in a single-item list.
        window.ConnectionViewModel.UpdateServer(serverA with { Name = "renamed" });
        Dispatcher.UIThread.RunJobs();

        // Mirrors what the real ContextMenu.Opening handler does - invoked
        // directly rather than through menu.Open(), which doesn't reliably
        // raise Opening in headless mode.
        window.SidebarForTesting.PopulateContextMenuForTesting(menu, container);
        var editItem = Assert.Single(menu.Items.OfType<MenuItem>(), item => (string?)item.Header == "編集");
        editItem.RaiseEvent(new Avalonia.Interactivity.RoutedEventArgs(MenuItem.ClickEvent));
        Dispatcher.UIThread.RunJobs();

        // OnEditServerClick opens a ConnectionWindow owned by this one -
        // reaching it at all (let alone for the right, renamed server)
        // proves the menu resolved its row's current DataContext rather
        // than a stale one captured when the menu was first built.
        var editor = window.OwnedWindows.OfType<ConnectionWindow>().Single();
        Assert.Equal("サーバーの編集", editor.Title);
        editor.Close();
        window.Close();
    });

    private static void AssertEveryRowHasItsOwnContextMenu(ListBox listBox, int expectedRows)
    {
        var containers = listBox.GetVisualDescendants().OfType<ListBoxItem>().ToList();
        Assert.Equal(expectedRows, containers.Count);
        Assert.All(containers, c => Assert.NotNull(c.ContextMenu));
        Assert.Equal(expectedRows, containers.Select(c => c.ContextMenu).Distinct().Count());
    }
}
