using Avalonia.Controls;
using Avalonia.Input;
using Avalonia.Layout;
using Avalonia.Media;
using FileExploder.Utilities;

namespace FileExploder.Views;

/// One clickable segment per path component between the connection's
/// remoteRoot and the current directory, each also a drop target - dropping
/// a row on an ancestor crumb is otherwise the only way to move something up
/// a level, since the parent directory has no row of its own to aim at.
public partial class BreadcrumbView : UserControl
{
    private static readonly IBrush DropTargetBrush = new SolidColorBrush(Colors.CornflowerBlue, 0.25);

    public event Action<string>? Navigated;

    /// Fired with the dropped path(s) and the crumb they were dropped on.
    public Action<IReadOnlyList<string>, string>? DropRequested;

    public BreadcrumbView()
    {
        InitializeComponent();
    }

    public void Update(string currentPath, string rootPath)
    {
        Crumbs.Children.Clear();

        var root = RemotePath.Standardized(rootPath);
        Crumbs.Children.Add(MakeCrumb("🏠", root));

        var current = RemotePath.Standardized(currentPath);
        // Navigation is confined to the root, but a mid-flight reconnection
        // can still render this with the two out of step; show just the
        // root rather than slicing a path that is not underneath it.
        if (!RemotePath.IsDescendant(current, root))
        {
            return;
        }

        var relative = root == "/" ? current[1..]
            : current == root ? ""
            : current[(root.Length + 1)..];
        var parts = RemotePath.SplitComponents(relative);

        var path = root;
        foreach (var part in parts)
        {
            path = RemotePath.Appending(part, path);
            Crumbs.Children.Add(new TextBlock
            {
                Text = "›", // chevron-right
                Foreground = Brushes.Gray,
                VerticalAlignment = VerticalAlignment.Center,
                FontSize = 12,
            });
            Crumbs.Children.Add(MakeCrumb(part, path));
        }
    }

    private Button MakeCrumb(string label, string destination)
    {
        var button = new Button
        {
            Content = label,
            Classes = { "borderless" },
            Padding = new Avalonia.Thickness(6, 2),
        };
        button.Click += (_, _) => Navigated?.Invoke(destination);

        DragDrop.SetAllowDrop(button, true);
        button.AddHandler(DragDrop.DragOverEvent, (_, e) =>
        {
            var accepts = e.DataTransfer.Formats.Contains(RemoteFileDrag.Format);
            e.DragEffects = accepts ? DragDropEffects.Move : DragDropEffects.None;
            button.Background = accepts ? DropTargetBrush : Brushes.Transparent;
        });
        button.AddHandler(DragDrop.DragLeaveEvent, (_, _) => button.Background = Brushes.Transparent);
        button.AddHandler(DragDrop.DropEvent, (_, e) =>
        {
            button.Background = Brushes.Transparent;
            var path = e.DataTransfer.Items.FirstOrDefault()?.TryGetRaw(RemoteFileDrag.Format) as string;
            if (path is null)
            {
                return;
            }
            DropRequested?.Invoke([path], destination);
        });

        return button;
    }
}
