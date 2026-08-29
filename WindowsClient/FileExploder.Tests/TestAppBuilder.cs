using Avalonia;
using Avalonia.Headless;
using FileExploder;

namespace FileExploder.Tests;

/// One-time, process-wide Avalonia setup for the UI smoke tests: headless
/// (no real window server needed), configured directly rather than through
/// Avalonia.Headless.XUnit - that package pulls in xunit v3, which conflicts
/// with the xunit v2 the rest of this test project (and its 80+ existing
/// tests) is built on. Plain Avalonia.Headless needs no such attribute or
/// package, just this one-time AppBuilder call before any Window is touched.
public static class HeadlessApp
{
    private static readonly Lock Gate = new();
    private static bool _initialized;

    public static void EnsureInitialized()
    {
        lock (Gate)
        {
            if (_initialized)
            {
                return;
            }
            AppBuilder.Configure<App>()
                .UseHeadless(new AvaloniaHeadlessPlatformOptions())
                .SetupWithoutStarting();
            _initialized = true;
        }
    }
}
