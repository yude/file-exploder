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
