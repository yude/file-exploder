import XCTest
@testable import file_exploder

final class RemotePathTests: XCTestCase {
    func testStandardizationIsLexicalAndRemote() {
        XCTAssertEqual(RemotePath.standardized("/srv//data/./a/../b/"), "/srv/data/b")
        XCTAssertEqual(RemotePath.standardized("/../../etc"), "/etc")
        XCTAssertEqual(RemotePath.standardized("relative/path"), "relative/path")
    }

    func testDescendantRequiresAComponentBoundary() {
        XCTAssertTrue(RemotePath.isDescendant("/srv/data", of: "/srv/data"))
        XCTAssertTrue(RemotePath.isDescendant("/srv/data/child", of: "/srv/data"))
        XCTAssertFalse(RemotePath.isDescendant("/srv/database", of: "/srv/data"))
        XCTAssertFalse(RemotePath.isDescendant("relative", of: "/srv/data"))
    }

    func testParentStopsAtRoot() {
        XCTAssertEqual(RemotePath.parent(of: "/srv/data"), "/srv")
        XCTAssertEqual(RemotePath.parent(of: "/"), "/")
    }
}

final class RFC3339ParserTests: XCTestCase {
    func testParsesGoNanosecondTimestamps() {
        XCTAssertNotNil(RFC3339Parser.date(from: "2026-08-27T12:34:56.123456789Z"))
        XCTAssertNotNil(RFC3339Parser.date(from: "2026-08-27T12:34:56.1+09:00"))
        XCTAssertNotNil(RFC3339Parser.date(from: "2026-08-27T12:34:56Z"))
    }

    func testRejectsInvalidTimestamp() {
        XCTAssertNil(RFC3339Parser.date(from: "not-a-date"))
    }
}

final class ShellEscapingTests: XCTestCase {
    func testEscapesSingleQuotes() {
        XCTAssertEqual("a'b".shellEscaped, "'a'\\''b'")
    }
}

final class ErrorLogTests: XCTestCase {
    func testNothingReportedWhenEmpty() {
        XCTAssertNil(ErrorLog().message)
    }

    func testListingErrorReadsOnItsOwn() {
        var log = ErrorLog()
        log.setListingError("接続できません")
        XCTAssertEqual(log.message, "接続できません")
    }

    /// The regression this split exists for: a listing that happens while an
    /// operation error is on screen must not take it away. Two overlapping
    /// operations both refresh, and every refresh replaces the listing error.
    func testAListingDoesNotDropOperationErrors() {
        var log = ErrorLog()
        log.addOperationError("削除エラー (a.txt)")

        log.setListingError(nil)
        XCTAssertEqual(log.message, "削除エラー (a.txt)")

        log.setListingError("一覧が取得できません")
        XCTAssertEqual(log.message, "削除エラー (a.txt)\n一覧更新エラー: 一覧が取得できません")

        log.setListingError(nil)
        XCTAssertEqual(log.message, "削除エラー (a.txt)")
    }

    func testConcurrentOperationsBothGetReported() {
        var log = ErrorLog()
        log.addOperationErrors(["削除エラー (a.txt)"])
        log.addOperationErrors(["名前変更エラー (b.txt)"])
        XCTAssertEqual(log.message, "削除エラー (a.txt)\n名前変更エラー (b.txt)")
    }

    func testOperationErrorsAreRetiredDeliberately() {
        var log = ErrorLog()
        log.addOperationError("削除エラー (a.txt)")
        log.setListingError("一覧が取得できません")

        log.clearOperationErrors()
        XCTAssertEqual(log.message, "一覧が取得できません")

        log.removeAll()
        XCTAssertNil(log.message)
    }
}
