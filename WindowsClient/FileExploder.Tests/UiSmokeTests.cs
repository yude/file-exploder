using Avalonia;
using Avalonia.Controls;
using Avalonia.Interactivity;
using Avalonia.Threading;
using Avalonia.VisualTree;
using FileExploder.Models;
using FileExploder.Services;
using FileExploder.Views;

namespace FileExploder.Tests;

/// Headless UI smoke tests: these exist because none of the ViewModel/
/// Service tests exercise a single line of .axaml - a binding path typo, a
/// missing converter, a DataGrid column that doesn't resolve, all compile
/// fine and only blow up at runtime. Running the real MainWindow (and a
/// real connect-and-list round trip through it) against Avalonia's headless
/// platform is the closest thing to "actually running the app" available in
/// this sandbox (no real display server, no Windows machine) - it is not a
/// substitute for actually running the published app on Windows before
/// trusting the UI, particularly the drag-and-drop wiring described in
/// FileListView's row-drag comments, which headless pointer simulation
/// does not attempt to cover here.
[Collection("Local SSH")]
public sealed class UiSmokeTests : IDisposable
{
    private readonly LocalSshFixture _fixture;
    private readonly string _scratchDir = Path.Combine(Path.GetTempPath(), "file-exploder-ui-smoke-" + Guid.NewGuid().ToString("N"));

    public UiSmokeTests(LocalSshFixture fixture)
    {
        _fixture = fixture;
        HeadlessApp.EnsureInitialized();
        Directory.CreateDirectory(_scratchDir);
    }

    public void Dispose()
    {
        if (Directory.Exists(_scratchDir))
        {
            Directory.Delete(_scratchDir, recursive: true);
        }
    }

    [Fact]
    public void MainWindowConstructsAndShowsWithoutThrowing()
    {
        var window = new MainWindow();
        window.Show();
        Dispatcher.UIThread.RunJobs();
        window.Close();
    }

    [Fact]
    public void ConnectingPopulatesTheRealFileGridAndBreadcrumb()
    {
        File.WriteAllText(Path.Combine(_scratchDir, "hello.txt"), "hi");
        Directory.CreateDirectory(Path.Combine(_scratchDir, "subdir"));

        var window = new MainWindow();
        window.Show();
        Dispatcher.UIThread.RunJobs();

        var server = new Server
        {
            Name = "smoke-test",
            Hostname = "localhost",
            Port = (ushort)_fixture.Port,
            Username = _fixture.Username,
            KeyPath = _fixture.PrivateKeyPath,
            RemoteRoot = _scratchDir,
        };

        // Not a plain `await`: ConnectAsync's continuations resume on
        // whatever SynchronizationContext was current when it was called -
        // Avalonia's dispatcher, here - and headless mode has no
        // background thread pumping that dispatcher's queue. Awaiting it
        // directly on this same (dispatcher) thread would deadlock the
        // continuation waiting for a pump that never runs while this
        // method is itself suspended. Driving the dispatcher manually while
        // polling for completion sidesteps that entirely.
        var connectTask = window.ConnectionViewModel.ConnectAsync(server, window.FileListViewModel);
        PumpUntilCompleted(connectTask, TimeSpan.FromSeconds(15));

        Assert.True(window.FileListViewModel.HasConnection, window.FileListViewModel.ErrorMessage);
        Assert.Contains(window.FileListViewModel.Files, f => f.Name == "hello.txt");
        Assert.Contains(window.FileListViewModel.Files, f => f.Name == "subdir");

        // Exercises FileListView's own PropertyChanged-driven rendering
        // pipeline, not just the ViewModel underneath it.
        var grid = FindDescendant<DataGrid>(window, "FilesGrid");
        Assert.NotNull(grid);
        Assert.True(grid!.IsVisible);
        Assert.Equal(2, grid.ItemsSource!.Cast<object>().Count());

        window.Close();
    }

    /// The "新しいウィンドウ" (Ctrl+N) menu item: each window is independent,
    /// but shares the saved-server list and settings underneath - ports
    /// FileExploderWindowCommands' openWindow(id: "main").
    [Fact]
    public void NewWindowMenuItemOpensAnIndependentSecondWindow()
    {
        var opened = new List<MainWindow>();
        void Capture(MainWindow w) => opened.Add(w);
        MainWindow.WindowOpenedForTesting += Capture;

        var first = new MainWindow();
        try
        {
            first.Show();
            Dispatcher.UIThread.RunJobs();

            // Headless mode never establishes a classic-desktop
            // ApplicationLifetime (that only happens via Program.Main's
            // StartWithClassicDesktopLifetime), so there is no window
            // registry to discover an opened window through - hence the
            // capture hook above instead.
            first.NewWindowMenuItemForTesting.RaiseEvent(new RoutedEventArgs(MenuItem.ClickEvent));
            Dispatcher.UIThread.RunJobs();

            var second = Assert.Single(opened);
            Assert.NotSame(first, second);
            Assert.NotSame(first.ConnectionViewModel, second.ConnectionViewModel);
        }
        finally
        {
            MainWindow.WindowOpenedForTesting -= Capture;
            foreach (var window in opened)
            {
                window.Close();
            }
            first.Close();
        }
    }

    /// The "設定..." (Ctrl+,) menu item: ports FileExploderApp.swift's
    /// Settings scene.
    [Fact]
    public void SettingsMenuItemOpensAWindowReflectingCurrentSettings()
    {
        var settingsFile = Path.GetTempFileName();
        File.Delete(settingsFile);
        AppSettings.UseFileForTesting(settingsFile);
        AppSettings.ShowHiddenFiles = true;
        AppSettings.RefreshInterval = 12;

        try
        {
            var window = new MainWindow();
            window.Show();
            Dispatcher.UIThread.RunJobs();

            window.SettingsMenuItemForTesting.RaiseEvent(new RoutedEventArgs(MenuItem.ClickEvent));
            Dispatcher.UIThread.RunJobs();

            var settingsWindow = window.OwnedWindows.OfType<SettingsWindow>().FirstOrDefault();
            Assert.NotNull(settingsWindow);

            var checkBox = FindDescendant<CheckBox>(settingsWindow!, "ShowHiddenFilesCheckBox")!;
            var slider = FindDescendant<Slider>(settingsWindow!, "RefreshIntervalSlider")!;
            Assert.True(checkBox.IsChecked);
            Assert.Equal(12, slider.Value);

            settingsWindow!.Close();
            window.Close();
        }
        finally
        {
            File.Delete(settingsFile);
        }
    }

    private static void PumpUntilCompleted(Task task, TimeSpan timeout)
    {
        var deadline = DateTime.UtcNow + timeout;
        while (!task.IsCompleted)
        {
            Dispatcher.UIThread.RunJobs();
            if (DateTime.UtcNow > deadline)
            {
                throw new TimeoutException($"Task did not complete within {timeout}.");
            }
            Thread.Sleep(10);
        }
        // Surfaces the real exception (with its own stack trace) instead of
        // an AggregateException wrapping it.
        task.GetAwaiter().GetResult();
    }

    private static T? FindDescendant<T>(Control root, string name) where T : Control =>
        root.GetVisualDescendants().OfType<T>().FirstOrDefault(c => c.Name == name);
}
