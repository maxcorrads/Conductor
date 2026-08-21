import XCTest
@testable import ConductorApp

final class TerminalLauncherTests: XCTestCase {
    func testShellQuotePreservesSingleQuotes() {
        XCTAssertEqual(shellQuote("one'two"), "'one'\"'\"'two'")
    }

    func testAppleScriptQuoteEscapesCommand() {
        XCTAssertEqual(appleScriptQuote("say \"hi\" \\ path"), "say \\\"hi\\\" \\\\ path")
    }

    func testTerminalLaunchScriptAssignsStableSessionTitle() {
        let script = TerminalLauncher.launchAppleScript(
            terminal: .terminal,
            command: "tmux attach-session -t demo--brain",
            session: "demo--brain"
        )
        XCTAssertTrue(script.contains("set custom title of targetTab to \"Conductor · demo--brain\""))
    }

    func testFocusScriptsOnlySelectExistingMatchingSessions() {
        let terminal = TerminalLauncher.focusAppleScript(terminal: .terminal, session: "demo--worker-1")
        XCTAssertTrue(terminal.contains("repeat with terminalWindow in windows"))
        XCTAssertTrue(terminal.contains("contains \"demo--worker-1\""))
        XCTAssertTrue(terminal.contains("return \"not-found\""))
        XCTAssertFalse(terminal.contains("do script"))

        let iterm = TerminalLauncher.focusAppleScript(terminal: .iterm, session: "demo--worker-1")
        XCTAssertTrue(iterm.contains("repeat with terminalSession in sessions of terminalTab"))
        XCTAssertTrue(iterm.contains("return \"not-found\""))
        XCTAssertFalse(iterm.contains("create window"))
    }
}
