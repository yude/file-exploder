using Avalonia.Threading;

namespace FileExploder.Tests;

internal static class TestUiHelpers
{
    /// Not a plain `await`: the awaited task's continuations resume on
    /// whatever SynchronizationContext was current when it was started -
    /// Avalonia's dispatcher, in every caller of this - and headless mode
    /// has no background thread pumping that dispatcher's queue. Awaiting
    /// directly on this same (dispatcher) thread would deadlock the
    /// continuation waiting for a pump that never runs while the calling
    /// method is itself suspended. Driving the dispatcher manually while
    /// polling for completion sidesteps that entirely.
    public static void PumpUntilCompleted(Task task, TimeSpan timeout)
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
}
