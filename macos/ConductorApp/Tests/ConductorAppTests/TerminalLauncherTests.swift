import XCTest
@testable import ConductorApp

final class TerminalLauncherTests: XCTestCase {
    func testShellQuotePreservesSingleQuotes() {
        XCTAssertEqual(shellQuote("one'two"), "'one'\"'\"'two'")
    }

    func testAppleScriptQuoteEscapesCommand() {
        XCTAssertEqual(appleScriptQuote("say \"hi\" \\ path"), "say \\\"hi\\\" \\\\ path")
    }
}
