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

    func testBase64TransportPreservesUnicodeNormalization() {
        let composed = "/mnt/store3/聞きたいこと\u{304c}ある.m2ts"
        let decomposed = "/mnt/store3/聞きたいこと\u{304b}\u{3099}ある.m2ts"

        XCTAssertNotEqual(Array(composed.utf8), Array(decomposed.utf8))
        XCTAssertEqual(Data(base64Encoded: composed.utf8Base64), Data(composed.utf8))
        XCTAssertEqual(Data(base64Encoded: decomposed.utf8Base64), Data(decomposed.utf8))
        XCTAssertNotEqual(composed.utf8Base64, decomposed.utf8Base64)
    }
}

final class RemotePathJSONNormalizationTests: XCTestCase {
    func testDecoderPreservesComposedPathBytesBeforeBase64Transport() throws {
        let data = Data(
            #"{"name":"\u304c","path":"/\u304c","size":0,"modificationDate":0,"isDirectory":false,"isSymlink":false,"permissions":420}"#.utf8
        )
        let item = try JSONDecoder.fileExploderDecoder().decode(RemoteFileJSON.self, from: data)

        XCTAssertEqual(Array(item.path.utf8), [0x2f, 0xe3, 0x81, 0x8c])
        XCTAssertEqual(Data(base64Encoded: item.path.utf8Base64), Data(item.path.utf8))
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

final class QueueJobClipboardLogTests: XCTestCase {
    func testIncludesAllUsefulJobDetails() {
        let job = QueueJob(
            id: "job-123",
            type: .copy,
            srcPath: "/srv/source.txt",
            dstPath: "/srv/destination.txt",
            mode: nil,
            status: .failed,
            error: "permission denied",
            createdAt: Date(timeIntervalSince1970: 0),
            startedAt: Date(timeIntervalSince1970: 1),
            completedAt: Date(timeIntervalSince1970: 2)
        )

        XCTAssertEqual(
            job.clipboardLog,
            """
            ID: job-123
            操作: コピー
            状態: 失敗
            内容: コピー /srv/source.txt -> /srv/destination.txt
            作成日時: 1970-01-01T00:00:00Z
            開始日時: 1970-01-01T00:00:01Z
            完了日時: 1970-01-01T00:00:02Z
            エラー: permission denied
            """
        )
    }

    func testOmitsUnavailableOptionalDetails() {
        let job = QueueJob(
            id: "job-456",
            type: .mkdir,
            srcPath: nil,
            dstPath: "/srv/new-directory",
            mode: nil,
            status: .pending,
            error: nil,
            createdAt: Date(timeIntervalSince1970: 0),
            startedAt: nil,
            completedAt: nil
        )

        XCTAssertFalse(job.clipboardLog.contains("開始日時:"))
        XCTAssertFalse(job.clipboardLog.contains("完了日時:"))
        XCTAssertFalse(job.clipboardLog.contains("エラー:"))
        XCTAssertTrue(job.clipboardLog.contains("内容: 作成 /srv/new-directory"))
    }
}
