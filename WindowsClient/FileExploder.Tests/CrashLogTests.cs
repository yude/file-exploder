using Avalonia.Controls;
using Avalonia.Interactivity;
using Avalonia.Threading;
using FileExploder.Services;

namespace FileExploder.Tests;

/// This app is a WinExe with no console - App.axaml.cs wires
/// Dispatcher.UIThread.UnhandledException specifically because that is
/// where an exception escaping a real button Click/SelectionChanged
/// handler surfaces (real user input is itself dispatched through
/// Dispatcher.UIThread, which is why this posts through it rather than
/// calling the handler directly - a direct call wouldn't exercise the same
/// path a real click does).
[Collection("Local SSH")]
public sealed class CrashLogTests : IDisposable
{
    private readonly string _logFile = Path.GetTempFileName();

    public CrashLogTests()
    {
        File.Delete(_logFile); // the log should tolerate a file that doesn't exist yet
        CrashLog.UseFileForTesting(_logFile);
    }

    public void Dispose()
    {
        if (File.Exists(_logFile))
        {
            File.Delete(_logFile);
        }
    }

    [Fact]
    public void AnExceptionFromADispatchedUiCallbackIsLoggedInsteadOfVanishing() => HeadlessApp.RunOnUiThread(() =>
    {
        var button = new Button();
        button.Click += (_, _) => throw new InvalidOperationException("boom");

        var window = new Window { Content = button };
        window.Show();
        Dispatcher.UIThread.RunJobs();

        Dispatcher.UIThread.Post(() => button.RaiseEvent(new RoutedEventArgs(Button.ClickEvent)));
        Dispatcher.UIThread.RunJobs();

        window.Close();

        Assert.True(File.Exists(_logFile));
        var content = File.ReadAllText(_logFile);
        Assert.Contains("boom", content);
        Assert.Contains("Dispatcher.UIThread.UnhandledException", content);
    });
}
