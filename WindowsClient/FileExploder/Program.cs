using Avalonia;
using System;
using FileExploder.Services;

namespace FileExploder;

sealed class Program
{
    // Initialization code. Don't use any Avalonia, third-party APIs or any
    // SynchronizationContext-reliant code before AppMain is called: things aren't initialized
    // yet and stuff might break.
    [STAThread]
    public static void Main(string[] args)
    {
        // Wired here, before anything else runs, so even a crash during
        // Avalonia's own startup gets logged. Dispatcher.UIThread's own
        // UnhandledException (the one that actually matters most - it is
        // what catches an exception escaping a button Click/SelectionChanged
        // handler, which is where most of this app's own logic runs) isn't
        // wired here: Dispatcher.UIThread doesn't exist yet at this point,
        // only after AppBuilder sets up the platform - see App.axaml.cs.
        AppDomain.CurrentDomain.UnhandledException += (_, e) =>
            CrashLog.Record("AppDomain.UnhandledException", e.ExceptionObject as Exception ?? new Exception(e.ExceptionObject?.ToString()));
        TaskScheduler.UnobservedTaskException += (_, e) =>
        {
            CrashLog.Record("TaskScheduler.UnobservedTaskException", e.Exception);
            e.SetObserved();
        };

        BuildAvaloniaApp()
            .StartWithClassicDesktopLifetime(args);
    }

    // Avalonia configuration, don't remove; also used by visual designer.
    public static AppBuilder BuildAvaloniaApp()
        => AppBuilder.Configure<App>()
            .UsePlatformDetect()
#if DEBUG
            .WithDeveloperTools()
#endif
            .WithInterFont()
            .LogToTrace();
}
