using System.Collections.Concurrent;
using Avalonia;
using Avalonia.Headless;
using FileExploder;

namespace FileExploder.Tests;

/// One-time, process-wide Avalonia setup for the UI smoke tests: headless
/// (no real window server needed), configured directly rather than through
/// Avalonia.Headless.XUnit - that package pulls in xunit v3, which conflicts
/// with the xunit v2 the rest of this test project (and its 80+ existing
/// tests) is built on.
///
/// Avalonia's dispatcher - and every control created against it - is
/// affinitized to exactly one OS thread process-wide. Avalonia.Headless.XUnit
/// handles this by running each test through its own dispatcher-thread
/// marshaling; without it, a test that merely calls SetupWithoutStarting()
/// once and then constructs Windows directly only works if xUnit happens to
/// run every such test on the same worker thread it did the first time -
/// which it does NOT guarantee, even within one serialized collection (it
/// showed up as a real, intermittent "the calling thread cannot access this
/// object because a different thread owns it" failure once enough tests
/// were added). RunOnUiThread below is this project's own replacement for
/// that marshaling: a single dedicated thread that SetupWithoutStarting()
/// runs on, and that every UI-touching test's body must run on too, via this
/// method - never by calling Avalonia APIs directly from a test method body.
public static class HeadlessApp
{
    private static readonly Lock Gate = new();
    private static readonly BlockingCollection<Action> Queue = new();
    private static bool _initialized;

    public static void EnsureInitialized()
    {
        lock (Gate)
        {
            if (_initialized)
            {
                return;
            }
            using var ready = new ManualResetEventSlim();
            var thread = new Thread(() =>
            {
                AppBuilder.Configure<App>()
                    .UseHeadless(new AvaloniaHeadlessPlatformOptions())
                    .SetupWithoutStarting();
                ready.Set();
                foreach (var action in Queue.GetConsumingEnumerable())
                {
                    action();
                }
            })
            {
                IsBackground = true,
                Name = "avalonia-headless-test-ui-thread",
            };
            thread.Start();
            ready.Wait();
            _initialized = true;
        }
    }

    /// Runs `action` on Avalonia's one dispatcher thread and blocks until it
    /// completes, re-throwing whatever it threw (with its original stack
    /// trace preserved via ExceptionDispatchInfo). Every test that
    /// constructs or touches an Avalonia control - directly, or indirectly
    /// through a ViewModel a View is subscribed to - must run its entire
    /// body through this, including its own Dispatcher.UIThread.RunJobs()
    /// pumping: that pumping only does anything meaningful on the thread
    /// the dispatcher actually owns.
    public static void RunOnUiThread(Action action)
    {
        EnsureInitialized();
        using var done = new ManualResetEventSlim();
        System.Runtime.ExceptionServices.ExceptionDispatchInfo? failure = null;
        Queue.Add(() =>
        {
            try
            {
                action();
            }
            catch (Exception ex)
            {
                failure = System.Runtime.ExceptionServices.ExceptionDispatchInfo.Capture(ex);
            }
            finally
            {
                done.Set();
            }
        });
        done.Wait();
        failure?.Throw();
    }
}
