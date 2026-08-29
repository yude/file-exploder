using FileExploder.Services;

namespace FileExploder.Tests;

/// Shares the "Local SSH" collection so this never runs concurrently with
/// another test also redirecting AppSettings' process-wide static state onto
/// a throwaway file.
[Collection("Local SSH")]
public sealed class AppSettingsTests : IDisposable
{
    private readonly string _file = Path.GetTempFileName();

    public AppSettingsTests()
    {
        File.Delete(_file); // the store should tolerate a file that doesn't exist yet
        AppSettings.UseFileForTesting(_file);
    }

    public void Dispose()
    {
        if (File.Exists(_file))
        {
            File.Delete(_file);
        }
    }

    [Fact]
    public void DefaultsMatchTheMacClientsAppStorageDefaults()
    {
        Assert.False(AppSettings.ShowHiddenFiles);
        Assert.Equal(5.0, AppSettings.RefreshInterval);
    }

    [Fact]
    public void WritingAValuePersistsItAcrossReads()
    {
        AppSettings.ShowHiddenFiles = true;
        AppSettings.RefreshInterval = 12.5;

        Assert.True(AppSettings.ShowHiddenFiles);
        Assert.Equal(12.5, AppSettings.RefreshInterval);
    }

    [Fact]
    public void PersistsAcrossARedirectToTheSameFile()
    {
        AppSettings.ShowHiddenFiles = true;
        AppSettings.RefreshInterval = 42;

        // Simulates a second window reading the same file this process
        // already wrote, by clearing the in-memory cache and reloading.
        AppSettings.UseFileForTesting(_file);

        Assert.True(AppSettings.ShowHiddenFiles);
        Assert.Equal(42, AppSettings.RefreshInterval);
    }

    [Fact]
    public void WritingASettingRaisesChanged()
    {
        var raised = 0;
        void Handler() => raised++;
        AppSettings.Changed += Handler;
        try
        {
            AppSettings.ShowHiddenFiles = true;
        }
        finally
        {
            AppSettings.Changed -= Handler;
        }

        Assert.Equal(1, raised);
    }

    [Fact]
    public void ACorruptSettingsFileFallsBackToDefaultsInsteadOfThrowing()
    {
        File.WriteAllText(_file, "not json");
        AppSettings.UseFileForTesting(_file);

        Assert.False(AppSettings.ShowHiddenFiles);
        Assert.Equal(5.0, AppSettings.RefreshInterval);
    }
}
