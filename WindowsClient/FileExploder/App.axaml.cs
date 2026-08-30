using Avalonia;
using Avalonia.Controls.ApplicationLifetimes;
using Avalonia.Markup.Xaml;
using Avalonia.Threading;
using FileExploder.Services;
using FileExploder.Views;

namespace FileExploder;

public partial class App : Application
{
    public override void Initialize()
    {
        // The one hook that matters most: this app has no console, and
        // almost all of its own logic runs from button Click/SelectionChanged
        // -style handlers that Avalonia's dispatcher invokes directly - an
        // exception escaping one of those otherwise just gets logged to
        // Avalonia's own internal trace (invisible without a debugger
        // attached) and silently leaves whatever it was doing half-finished,
        // rather than crashing loudly. Marking Handled = true keeps the app
        // itself running afterward, on the view that whatever broke is
        // better surfaced via this log than by taking down a window the
        // user might still be relying on for an unrelated tab/connection.
        Dispatcher.UIThread.UnhandledException += (_, e) =>
        {
            DiagnosticLog.LogException("Dispatcher.UIThread.UnhandledException", e.Exception);
            e.Handled = true;
        };

        AvaloniaXamlLoader.Load(this);
    }

    public override void OnFrameworkInitializationCompleted()
    {
        if (ApplicationLifetime is IClassicDesktopStyleApplicationLifetime desktop)
        {
            desktop.MainWindow = new MainWindow();
        }

        base.OnFrameworkInitializationCompleted();
    }
}