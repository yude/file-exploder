using Avalonia.Controls;
using Avalonia.Media;

namespace FileExploder.Views;

/// A destructive-action confirmation, shared by the two places the macOS
/// client shows a confirmationDialog: deleting files and deleting a saved
/// server.
public partial class ConfirmWindow : Window
{
    public ConfirmWindow()
    {
        InitializeComponent();
        ConfirmButton.Click += (_, _) => Close(true);
        CancelButton.Click += (_, _) => Close(false);
    }

    public static async Task<bool> ShowAsync(Window owner, string header, string message, string confirmLabel)
    {
        var window = new ConfirmWindow
        {
            Title = header,
        };
        window.HeaderText.Text = header;
        window.MessageText.Text = message;
        window.ConfirmButton.Content = confirmLabel;
        window.ConfirmButton.Foreground = Brushes.Red;

        return await window.ShowDialog<bool>(owner);
    }
}
