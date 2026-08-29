using Avalonia.Controls;
using FileExploder.Services;

namespace FileExploder.Views;

/// Ports FileExploderApp.swift's SettingsView (opened from the macOS Settings
/// scene) - writes straight through to AppSettings as the user interacts,
/// the same way SettingsView's Toggle/Slider bind directly to @AppStorage
/// with no separate save step. Every open window's FileListViewModel picks
/// up the change via AppSettings.Changed.
public partial class SettingsWindow : Window
{
    public SettingsWindow()
    {
        InitializeComponent();
        ShowHiddenFilesCheckBox.IsChecked = AppSettings.ShowHiddenFiles;
        RefreshIntervalSlider.Value = AppSettings.RefreshInterval;
        UpdateRefreshIntervalLabel();

        ShowHiddenFilesCheckBox.IsCheckedChanged += (_, _) =>
            AppSettings.ShowHiddenFiles = ShowHiddenFilesCheckBox.IsChecked ?? false;
        RefreshIntervalSlider.PropertyChanged += (_, e) =>
        {
            if (e.Property == Slider.ValueProperty)
            {
                AppSettings.RefreshInterval = RefreshIntervalSlider.Value;
                UpdateRefreshIntervalLabel();
            }
        };
    }

    private void UpdateRefreshIntervalLabel() =>
        RefreshIntervalLabel.Text = $"更新間隔 ({(int)RefreshIntervalSlider.Value} 秒)";
}
