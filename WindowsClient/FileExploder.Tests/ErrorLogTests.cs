using FileExploder.Models;

namespace FileExploder.Tests;

public class ErrorLogTests
{
    [Fact]
    public void NothingReportedWhenEmpty()
    {
        Assert.Null(new ErrorLog().Message);
    }

    [Fact]
    public void ListingErrorReadsOnItsOwn()
    {
        var log = new ErrorLog().SetListingError("接続できません");
        Assert.Equal("接続できません", log.Message);
    }

    /// The regression this split exists for: a listing that happens while an
    /// operation error is on screen must not take it away. Two overlapping
    /// operations both refresh, and every refresh replaces the listing error.
    [Fact]
    public void AListingDoesNotDropOperationErrors()
    {
        var log = new ErrorLog().AddOperationError("削除エラー (a.txt)");

        log = log.SetListingError(null);
        Assert.Equal("削除エラー (a.txt)", log.Message);

        log = log.SetListingError("一覧が取得できません");
        Assert.Equal("削除エラー (a.txt)\n一覧更新エラー: 一覧が取得できません", log.Message);

        log = log.SetListingError(null);
        Assert.Equal("削除エラー (a.txt)", log.Message);
    }

    [Fact]
    public void ConcurrentOperationsBothGetReported()
    {
        var log = new ErrorLog()
            .AddOperationErrors(["削除エラー (a.txt)"])
            .AddOperationErrors(["名前変更エラー (b.txt)"]);
        Assert.Equal("削除エラー (a.txt)\n名前変更エラー (b.txt)", log.Message);
    }

    [Fact]
    public void OperationErrorsAreRetiredDeliberately()
    {
        var log = new ErrorLog()
            .AddOperationError("削除エラー (a.txt)")
            .SetListingError("一覧が取得できません");

        log = log.ClearOperationErrors();
        Assert.Equal("一覧が取得できません", log.Message);

        log = log.Clear();
        Assert.Null(log.Message);
    }

    /// The listing error stands alone so the view can show a full-page
    /// failure; operation errors stand alone so the view can show a banner
    /// and keep the file list on screen.
    [Fact]
    public void TheTwoKindsAreReadableSeparately()
    {
        var log = new ErrorLog();
        Assert.Null(log.OperationMessage);
        Assert.Null(log.ListingError);

        log = log.AddOperationError("削除エラー (a.txt)");
        Assert.Equal("削除エラー (a.txt)", log.OperationMessage);
        Assert.Null(log.ListingError);

        log = log.AddOperationError("名前変更エラー (b.txt)");
        Assert.Equal("削除エラー (a.txt)\n名前変更エラー (b.txt)", log.OperationMessage);

        log = log.SetListingError("一覧が取得できません");
        Assert.Equal("一覧が取得できません", log.ListingError);
        Assert.Equal("削除エラー (a.txt)\n名前変更エラー (b.txt)", log.OperationMessage);

        log = log.ClearOperationErrors();
        Assert.Null(log.OperationMessage);
        Assert.Equal("一覧が取得できません", log.ListingError);
    }
}
