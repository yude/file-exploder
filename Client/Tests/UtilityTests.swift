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

final class RemoteFileIdentityTests: XCTestCase {
    private func file(named name: String, in directory: String = "/srv") -> RemoteFile {
        RemoteFile(
            name: name,
            path: directory + "/" + name,
            size: 0,
            modificationDate: Date(timeIntervalSince1970: 0),
            isDirectory: false,
            isSymlink: false,
            permissions: FilePermissions.from(octal: 0o644)
        )
    }

    /// A Linux server holds these as two separate files; Swift compares their
    /// paths as equal, so the id has to be what keeps them apart.
    func testCanonicallyEquivalentNamesGetDistinctIdentities() {
        let composed = file(named: "caf\u{00e9}.txt")      // NFC
        let decomposed = file(named: "cafe\u{0301}.txt")   // NFD

        XCTAssertEqual(composed.path, decomposed.path, "Swift String equality is canonical — the premise of this test")
        XCTAssertNotEqual(composed.id, decomposed.id)
        XCTAssertNotEqual(composed, decomposed)
        XCTAssertEqual(Set([composed, decomposed]).count, 2)
        XCTAssertEqual(Set([composed.id, decomposed.id]).count, 2)
    }

    func testIdentityIsStableAndDistinguishesDifferentPaths() {
        XCTAssertEqual(file(named: "a.txt").id, file(named: "a.txt").id)
        XCTAssertNotEqual(file(named: "a.txt").id, file(named: "b.txt").id)
        XCTAssertNotEqual(file(named: "a.txt", in: "/srv").id, file(named: "a.txt", in: "/srv2").id)
    }
}

final class ErrorLogSeparationTests: XCTestCase {
    /// The listing error stands alone so the view can show a full-page failure;
    /// operation errors stand alone so the view can show a banner and keep the
    /// file list on screen.
    func testTheTwoKindsAreReadableSeparately() {
        var log = ErrorLog()
        XCTAssertNil(log.operationMessage)
        XCTAssertNil(log.listingError)

        log.addOperationError("削除エラー (a.txt)")
        XCTAssertEqual(log.operationMessage, "削除エラー (a.txt)")
        XCTAssertNil(log.listingError, "a failed delete must not blank the listing")

        log.addOperationError("名前変更エラー (b.txt)")
        XCTAssertEqual(log.operationMessage, "削除エラー (a.txt)\n名前変更エラー (b.txt)")

        log.setListingError("一覧が取得できません")
        XCTAssertEqual(log.listingError, "一覧が取得できません")
        XCTAssertEqual(log.operationMessage, "削除エラー (a.txt)\n名前変更エラー (b.txt)")

        log.clearOperationErrors()
        XCTAssertNil(log.operationMessage)
        XCTAssertEqual(log.listingError, "一覧が取得できません")
    }
}

final class RemotePathComponentTests: XCTestCase {
    func testAcceptsOrdinaryNames() {
        for name in ["file.txt", "フォルダ", "a b", "-dash", "..hidden", "a.b.c", "🙂"] {
            XCTAssertTrue(RemotePath.isValidComponent(name), name)
        }
    }

    func testRejectsAnythingThatIsNotOneComponent() {
        for name in ["", ".", "..", "/", "a/b", "/leading", "trailing/", "nul\u{0000}"] {
            XCTAssertFalse(RemotePath.isValidComponent(name), name)
        }
    }

    /// Swift groups "/" plus a combining mark into a single Character that is
    /// not "/", so a Character-level check let these through.
    func testRejectsASeparatorHiddenBehindACombiningMark() {
        XCTAssertFalse("/\u{0301}".contains("/"), "the premise: Character-level containment misses it")
        XCTAssertFalse(RemotePath.isValidComponent("/\u{0301}"))
        XCTAssertFalse(RemotePath.isValidComponent("a/\u{0301}b"))
        XCTAssertFalse(RemotePath.isValidComponent("etc/\u{0301}passwd"))
    }
}

final class RemotePathMoveTests: XCTestCase {
    func testAcceptsAMoveIntoAnotherDirectory() {
        XCTAssertTrue(RemotePath.canMove("/srv/a.txt", into: "/srv/archive"))
        XCTAssertTrue(RemotePath.canMove("/srv/docs", into: "/srv/archive"))
        XCTAssertTrue(RemotePath.canMove("/srv/docs/deep/file", into: "/srv"))
        XCTAssertTrue(RemotePath.canMove("/srv/a.txt", into: "/"))
    }

    func testRefusesAMoveIntoTheDirectoryItIsAlreadyIn() {
        XCTAssertFalse(RemotePath.canMove("/srv/a.txt", into: "/srv"))
        XCTAssertFalse(RemotePath.canMove("/srv/a.txt", into: "/srv/"))
        XCTAssertFalse(RemotePath.canMove("/srv/a.txt", into: "/srv/./"))
        XCTAssertFalse(RemotePath.canMove("/a.txt", into: "/"))
    }

    /// The drop the server rejects with "cannot move a directory into itself".
    func testRefusesADirectoryDroppedIntoItsOwnSubtree() {
        XCTAssertFalse(RemotePath.canMove("/srv/docs", into: "/srv/docs"))
        XCTAssertFalse(RemotePath.canMove("/srv/docs", into: "/srv/docs/nested"))
        XCTAssertFalse(RemotePath.canMove("/srv/docs", into: "/srv/docs/a/b/c"))
        // A sibling that merely shares a prefix is fine.
        XCTAssertTrue(RemotePath.canMove("/srv/docs", into: "/srv/docs-backup"))
    }
}

final class DroppedRowResolutionTests: XCTestCase {
    /// A drop carries paths, and paths have to be turned back into rows by
    /// identity. Matching paths directly would let one dragged row select both
    /// halves of an NFC/NFD pair, and move a file the user never dragged.
    func testIdentityDistinguishesCanonicallyEqualPaths() {
        let composed = "/srv/caf\u{00e9}.txt"
        let decomposed = "/srv/cafe\u{0301}.txt"

        XCTAssertEqual(composed, decomposed, "the premise: Swift compares these as equal")
        XCTAssertEqual(Set([composed, decomposed]).count, 1, "so a Set of paths collapses them")

        let ids = Set([composed, decomposed].map(RemoteFile.identity(for:)))
        XCTAssertEqual(ids.count, 2, "identities must keep them apart")
        XCTAssertFalse(ids.isEmpty)

        // Dragging one must resolve to exactly one of them.
        let dragged = Set([RemoteFile.identity(for: composed)])
        let matched = [composed, decomposed].filter { dragged.contains(RemoteFile.identity(for: $0)) }
        XCTAssertEqual(matched.count, 1)
    }

    func testIdentityIsStableForTheSamePath() {
        XCTAssertEqual(RemoteFile.identity(for: "/srv/a"), RemoteFile.identity(for: "/srv/a"))
        XCTAssertNotEqual(RemoteFile.identity(for: "/srv/a"), RemoteFile.identity(for: "/srv/b"))
    }
}

final class ServerAuthTypeTests: XCTestCase {
    private func decode(_ raw: String) throws -> Server.AuthType {
        try JSONDecoder().decode(Server.AuthType.self, from: Data("\"\(raw)\"".utf8))
    }

    /// The stored value must not be the label, or changing the wording would
    /// make every saved server undecodable.
    func testStoresAStableValueRatherThanTheLabel() throws {
        let encoded = try JSONEncoder().encode(Server.AuthType.sshKey)
        XCTAssertEqual(String(decoding: encoded, as: UTF8.self), "\"sshKey\"")
        XCTAssertEqual(Server.AuthType.sshKey.displayName, "SSHキー")
    }

    func testStillReadsListsSavedBeforeTheValueWasSeparated() throws {
        XCTAssertEqual(try decode("SSHキー"), .sshKey)
        XCTAssertEqual(try decode("sshKey"), .sshKey)
    }

    func testRejectsSomethingItDoesNotRecognise() {
        XCTAssertThrowsError(try decode("password"))
    }

    func testAWholeSavedServerRoundTrips() throws {
        let server = Server(
            name: "srv",
            hostname: "example.test",
            port: 2222,
            username: "user",
            keyPath: "/home/user/.ssh/id_ed25519",
            remoteRoot: "/srv"
        )
        let restored = try JSONDecoder().decode(Server.self, from: JSONEncoder().encode(server))
        XCTAssertEqual(restored, server)
    }
}
