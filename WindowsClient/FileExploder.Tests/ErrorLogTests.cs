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

    /// A bulk operation reports one error per file it could not finish, with
    /// no bound on how many that is - every file in a directory the user
    /// cannot write to, say. All of them used to be joined into the single
    /// string the banner renders.
    [Fact]
    public void ABulkFailureIsSummarisedRatherThanRenderedInFull()
    {
        var log = new ErrorLog().AddOperationErrors(
            Enumerable.Range(0, 5000).Select(i => $"削除エラー (file-{i}.txt): 権限がありません"));

        var lines = log.OperationMessage!.Split('\n');
        Assert.Equal(ErrorLog.MaxOperationErrors + 1, lines.Length);
        Assert.Equal("削除エラー (file-0.txt): 権限がありません", lines[0]);
        Assert.Equal($"ほか {5000 - ErrorLog.MaxOperationErrors} 件のエラー", lines[^1]);

        // The whole point: what reaches a single text control stays small.
        Assert.True(log.OperationMessage!.Length < 2000, $"banner was {log.OperationMessage!.Length} characters");
    }

    /// The count has to keep accumulating across separate operations, not
    /// reset each time more errors arrive.
    [Fact]
    public void TheOverflowCountAccumulatesAcrossOperations()
    {
        var log = new ErrorLog();
        for (var round = 0; round < 3; round++)
        {
            log = log.AddOperationErrors(Enumerable.Range(0, ErrorLog.MaxOperationErrors).Select(i => $"e{round}-{i}"));
        }

        Assert.Equal(ErrorLog.MaxOperationErrors, log.OperationErrors.Count);
        Assert.Equal(ErrorLog.MaxOperationErrors * 2, log.DroppedOperationErrors);
        Assert.EndsWith($"ほか {ErrorLog.MaxOperationErrors * 2} 件のエラー", log.OperationMessage);

        // ...and retiring them retires the count too.
        log = log.ClearOperationErrors();
        Assert.Null(log.OperationMessage);
        Assert.Equal(0, log.DroppedOperationErrors);
    }

    /// Under the cap nothing is added: the summary line only appears once
    /// something has actually been left out.
    [Fact]
    public void NoSummaryLineWhenNothingWasLeftOut()
    {
        var log = new ErrorLog().AddOperationErrors(
            Enumerable.Range(0, ErrorLog.MaxOperationErrors).Select(i => $"e{i}"));
        Assert.Equal(ErrorLog.MaxOperationErrors, log.OperationMessage!.Split('\n').Length);
        Assert.DoesNotContain("ほか", log.OperationMessage);
    }
}
