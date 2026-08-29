using Avalonia.Controls;
using Avalonia.Interactivity;

namespace FileExploder.Views;

/// A single-textfield modal prompt, shared by every place the macOS client
/// shows one of its near-identical NewFolderSheet/RenameSheet/MoveSheet
/// forms - the differences between them are only the header, placeholder,
/// initial text and confirm-button label.
public partial class TextPromptWindow : Window
{
    private string? _result;

    public TextPromptWindow()
    {
        InitializeComponent();
        Input.KeyDown += (_, e) =>
        {
            if (e.Key == Avalonia.Input.Key.Enter && ConfirmButton.IsEnabled)
            {
                Confirm();
            }
        };
        Input.TextChanged += (_, _) => UpdateConfirmEnabled();
        ConfirmButton.Click += (_, _) => Confirm();
        CancelButton.Click += (_, _) => Close(null);
    }

    private void UpdateConfirmEnabled() => ConfirmButton.IsEnabled = !string.IsNullOrEmpty(Input.Text);

    private void Confirm()
    {
        _result = Input.Text;
        Close(_result);
    }

    public static async Task<string?> ShowAsync(
        Window owner,
        string header,
        string placeholder,
        string confirmLabel,
        string initialText = "")
    {
        var window = new TextPromptWindow
        {
            Title = header,
        };
        window.HeaderText.Text = header;
        window.Input.PlaceholderText = placeholder;
        window.Input.Text = initialText;
        window.ConfirmButton.Content = confirmLabel;
        window.UpdateConfirmEnabled();

        var result = await window.ShowDialog<string?>(owner);
        return string.IsNullOrEmpty(result) ? null : result;
    }
}
