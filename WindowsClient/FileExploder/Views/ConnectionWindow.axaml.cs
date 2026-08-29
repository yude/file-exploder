using Avalonia.Controls;
using Avalonia.Platform.Storage;
using FileExploder.Models;
using FileExploder.Utilities;
using FileExploder.ViewModels;

namespace FileExploder.Views;

/// Add/edit a saved server. Ports ConnectionSheet.swift's validation rules
/// exactly: they exist to keep an unusable server (an empty host, a key
/// path containing a NUL byte, a root that isn't even absolute) out of the
/// saved list in the first place, rather than surfacing as a connection
/// failure later.
public partial class ConnectionWindow : Window
{
    private ConnectionViewModel _connectionVM = null!;
    private FileListViewModel _fileListVM = null!;
    private Server? _editing;

    public ConnectionWindow()
    {
        InitializeComponent();
        AuthTypeBox.ItemsSource = new[] { AuthType.SshKey.DisplayName() };
        AuthTypeBox.SelectedIndex = 0;

        NameBox.TextChanged += (_, _) => UpdateSaveEnabled();
        HostnameBox.TextChanged += (_, _) => UpdateSaveEnabled();
        UsernameBox.TextChanged += (_, _) => UpdateSaveEnabled();
        KeyPathBox.TextChanged += (_, _) => UpdateSaveEnabled();
        RemoteRootBox.TextChanged += (_, _) => UpdateSaveEnabled();
        PortBox.TextChanged += (_, _) =>
        {
            var filtered = new string([.. PortBox.Text?.Where(char.IsAsciiDigit) ?? []]);
            if (filtered != PortBox.Text)
            {
                var caret = PortBox.CaretIndex;
                PortBox.Text = filtered;
                PortBox.CaretIndex = Math.Min(caret, filtered.Length);
            }
            UpdateSaveEnabled();
        };

        BrowseButton.Click += OnBrowseAsync;
        SaveButton.Click += (_, _) => Save();
        CancelButton.Click += (_, _) => Close();
    }

    private async void OnBrowseAsync(object? sender, Avalonia.Interactivity.RoutedEventArgs e)
    {
        var topLevel = GetTopLevel(this);
        if (topLevel?.StorageProvider is not { } storageProvider)
        {
            return;
        }
        var files = await storageProvider.OpenFilePickerAsync(new FilePickerOpenOptions
        {
            Title = "SSHキーを選択",
            AllowMultiple = false,
        });
        if (files.Count > 0 && files[0].TryGetLocalPath() is { } path)
        {
            KeyPathBox.Text = path;
        }
    }

    private ushort? ValidPort => ushort.TryParse(PortBox.Text, out var value) && value > 0 ? value : null;

    private string TrimmedRoot => (RemoteRootBox.Text ?? "").Trim();

    private bool KeyPathIsValid
    {
        get
        {
            var trimmed = (KeyPathBox.Text ?? "").Trim();
            return !trimmed.Contains('\0') && (trimmed.Length == 0 || trimmed.StartsWith('/'));
        }
    }

    private bool ConnectionFieldsAreValid
    {
        get
        {
            var trimmedHost = (HostnameBox.Text ?? "").Trim();
            var trimmedUser = (UsernameBox.Text ?? "").Trim();
            return trimmedHost.Length > 0
                && !trimmedHost.Any(c => char.IsWhiteSpace(c) || c == '\0')
                && trimmedUser.Length > 0
                && !trimmedUser.Any(c => char.IsWhiteSpace(c) || c == '\0' || c == '@');
        }
    }

    private bool CanSave =>
        !string.IsNullOrWhiteSpace(NameBox.Text)
        && ConnectionFieldsAreValid
        && ValidPort is not null
        && TrimmedRoot.StartsWith('/') && !TrimmedRoot.Contains('\0')
        && KeyPathIsValid;

    private void UpdateSaveEnabled() => SaveButton.IsEnabled = CanSave;

    private void Save()
    {
        if (ValidPort is not { } port)
        {
            return;
        }
        // The root lives on the server; the key path is local, so only the
        // latter may be standardized against this machine's filesystem.
        var normalizedRoot = RemotePath.Standardized(TrimmedRoot);
        var trimmedKeyPath = (KeyPathBox.Text ?? "").Trim();
        var normalizedKeyPath = trimmedKeyPath.Length == 0 ? null : Path.GetFullPath(trimmedKeyPath);

        var newServer = new Server
        {
            Name = (NameBox.Text ?? "").Trim(),
            Hostname = (HostnameBox.Text ?? "").Trim(),
            Port = port,
            Username = (UsernameBox.Text ?? "").Trim(),
            AuthType = AuthType.SshKey,
            KeyPath = normalizedKeyPath,
            RemoteRoot = normalizedRoot,
        };

        if (_editing is { } existing)
        {
            if (_connectionVM.ActiveServerId == existing.Id)
            {
                _fileListVM.Disconnect();
                _connectionVM.Disconnect();
            }
            _connectionVM.UpdateServer(newServer with { Id = existing.Id });
        }
        else
        {
            _connectionVM.AddServer(newServer);
        }

        Close();
    }

    public static Task ShowAsync(Window owner, ConnectionViewModel connectionVM, FileListViewModel fileListVM, Server? server)
    {
        var window = new ConnectionWindow
        {
            _connectionVM = connectionVM,
            _fileListVM = fileListVM,
            _editing = server,
        };
        window.HeaderText.Text = server is null ? "サーバーの追加" : "サーバーの編集";
        window.Title = window.HeaderText.Text;
        window.SaveButton.Content = server is null ? "追加" : "保存";

        if (server is { } existing)
        {
            window.NameBox.Text = existing.Name;
            window.HostnameBox.Text = existing.Hostname;
            window.PortBox.Text = existing.Port.ToString();
            window.UsernameBox.Text = existing.Username;
            window.KeyPathBox.Text = existing.KeyPath ?? "";
            window.RemoteRootBox.Text = existing.RemoteRoot;
        }
        else
        {
            window.PortBox.Text = Server.DefaultPort.ToString();
            window.RemoteRootBox.Text = "/";
        }
        window.UpdateSaveEnabled();

        return window.ShowDialog(owner);
    }
}
