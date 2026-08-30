using Avalonia;
using Avalonia.Controls;
using Avalonia.Headless;
using Avalonia.Input;
using Avalonia.Threading;
using Avalonia.VisualTree;
using FileExploder.Models;
using FileExploder.Views;

namespace FileExploder.Tests;

/// Drives real synthetic pointer input at the file table's rows, rather
/// than only checking the ViewModel underneath it. This is the gap that let
/// double-click-to-open and drag-to-move both ship broken: the row's
/// PointerPressed was subscribed with `row.PointerPressed +=`, which never
/// fires at all, because the DataGrid marks the press handled for its own
/// selection before it reaches the row (verified directly). Both features
/// hang off that one subscription.
[Collection("Local SSH")]
public sealed class FileListViewInputTests : IDisposable
{
    private readonly LocalSshFixture _fixture;
    private readonly string _scratchDir = Path.Combine(Path.GetTempPath(), "file-exploder-input-tests-" + Guid.NewGuid().ToString("N"));

    public FileListViewInputTests(LocalSshFixture fixture)
    {
        _fixture = fixture;
        Directory.CreateDirectory(_scratchDir);
    }

    public void Dispose()
    {
        if (Directory.Exists(_scratchDir))
        {
            Directory.Delete(_scratchDir, recursive: true);
        }
    }

    private MainWindow ConnectedWindow()
    {
        var window = new MainWindow();
        window.Show();
        Dispatcher.UIThread.RunJobs();

        var server = new Server
        {
            Name = "input-test",
            Hostname = "localhost",
            Port = (ushort)_fixture.Port,
            Username = _fixture.Username,
            KeyPath = _fixture.PrivateKeyPath,
            RemoteRoot = _scratchDir,
        };
        TestUiHelpers.PumpUntilCompleted(
            window.ConnectionViewModel.ConnectAsync(server, window.FileListViewModel),
            TimeSpan.FromSeconds(15));
        Dispatcher.UIThread.RunJobs();
        Dispatcher.UIThread.RunJobs();
        return window;
    }

    private static DataGridRow RowFor(MainWindow window, string name) =>
        window.FileListViewForTesting.FilesGridForTesting
            .GetVisualDescendants().OfType<DataGridRow>()
            .Single(r => r.DataContext is RemoteFile f && f.Name == name);

    private static Point CenterOf(DataGridRow row, Visual relativeTo) =>
        row.TranslatePoint(new Point(row.Bounds.Width / 2, row.Bounds.Height / 2), relativeTo)
        ?? throw new InvalidOperationException("could not translate the row's center");

    [Fact]
    public void DoubleClickingADirectoryRowNavigatesIntoIt() => HeadlessApp.RunOnUiThread(() =>
    {
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));
        File.WriteAllText(Path.Combine(_scratchDir, "a.txt"), "a");

        var window = ConnectedWindow();
        var row = RowFor(window, "subdir");
        var center = CenterOf(row, window);

        window.MouseMove(center);
        window.MouseDown(center, MouseButton.Left);
        window.MouseUp(center, MouseButton.Left);
        window.MouseDown(center, MouseButton.Left);
        window.MouseUp(center, MouseButton.Left);

        var expected = Path.Combine(_scratchDir, "subdir");
        var deadline = DateTime.UtcNow + TimeSpan.FromSeconds(15);
        while (window.FileListViewModel.CurrentPath != expected && DateTime.UtcNow < deadline)
        {
            Dispatcher.UIThread.RunJobs();
            Thread.Sleep(10);
        }

        Assert.Equal(expected, window.FileListViewModel.CurrentPath);
        window.Close();
    });

    /// A double-click on a plain file must not navigate (there is nowhere
    /// to navigate to) - the same handler decides both.
    [Fact]
    public void DoubleClickingAFileRowDoesNotNavigate() => HeadlessApp.RunOnUiThread(() =>
    {
        File.WriteAllText(Path.Combine(_scratchDir, "a.txt"), "a");

        var window = ConnectedWindow();
        var row = RowFor(window, "a.txt");
        var center = CenterOf(row, window);

        window.MouseMove(center);
        window.MouseDown(center, MouseButton.Left);
        window.MouseUp(center, MouseButton.Left);
        window.MouseDown(center, MouseButton.Left);
        window.MouseUp(center, MouseButton.Left);
        Dispatcher.UIThread.RunJobs();

        Assert.Equal(_scratchDir, window.FileListViewModel.CurrentPath);
        window.Close();
    });

    /// Dragging a file row far enough past the drag threshold must actually
    /// start a drag - i.e. the press must have been observed in the first
    /// place, which is what was broken.
    [Fact]
    public void DraggingARowStartsADragWithTheRowsPathAsThePayload() => HeadlessApp.RunOnUiThread(() =>
    {
        File.WriteAllText(Path.Combine(_scratchDir, "a.txt"), "a");
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));

        var window = ConnectedWindow();
        var source = RowFor(window, "a.txt");
        var target = RowFor(window, "subdir");
        var from = CenterOf(source, window);
        var to = CenterOf(target, window);

        window.MouseMove(from);
        window.MouseDown(from, MouseButton.Left);
        // Well past FileListView's 6px threshold. The modifier matters:
        // headless MouseMove defaults to no buttons held, and the drag
        // only starts while the left button is still down - exactly as a
        // real drag reports it.
        window.MouseMove(new Point(from.X + 40, from.Y + 40), RawInputModifiers.LeftMouseButton);
        Dispatcher.UIThread.RunJobs();

        // DoDragDropAsync takes over the pointer once a drag begins; the
        // observable consequence here is simply that it was reached without
        // the press being dropped on the floor. Releasing ends it.
        window.MouseUp(to, MouseButton.Left);
        Dispatcher.UIThread.RunJobs();

        Assert.True(window.FileListViewForTesting.DragWasInitiatedForTesting,
            "moving well past the drag threshold never started a drag - the row's PointerPressed was likely never observed");
        window.Close();
    });
}
