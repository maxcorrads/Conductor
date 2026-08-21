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
        let terminal = TerminalLauncher.focusAppleScript(
            terminal: .terminal,
            session: "demo--worker-1",
            clientTTYs: ["/dev/ttys001", "/dev/ttys009"]
        )
        XCTAssertTrue(terminal.contains("repeat with terminalWindow in windows"))
        XCTAssertTrue(terminal.contains("custom title of terminalTab is \"Conductor · demo--worker-1\""))
        XCTAssertFalse(terminal.contains("contains \"demo--worker-1\""))
        XCTAssertFalse(terminal.contains("demo--worker-10"))
        XCTAssertTrue(terminal.contains("targetTTYs contains (tty of terminalTab)"))
        XCTAssertTrue(terminal.contains("{\"/dev/ttys001\", \"/dev/ttys009\"}"))
        XCTAssertTrue(terminal.contains("return \"not-found\""))
        XCTAssertFalse(terminal.contains("do script"))

        let iterm = TerminalLauncher.focusAppleScript(
            terminal: .iterm,
            session: "demo--worker-1",
            clientTTYs: ["/dev/ttys001"]
        )
        XCTAssertTrue(iterm.contains("repeat with terminalSession in sessions of terminalTab"))
        XCTAssertTrue(iterm.contains("name of terminalSession is \"Conductor · demo--worker-1\""))
        XCTAssertFalse(iterm.contains("contains \"demo--worker-1\""))
        XCTAssertFalse(iterm.contains("demo--worker-10"))
        XCTAssertTrue(iterm.contains("targetTTYs contains (tty of terminalSession)"))
        XCTAssertTrue(iterm.contains("return \"not-found\""))
        XCTAssertFalse(iterm.contains("create window"))
    }

    func testTmuxClientTTYParserUsesExactSessionIdentity() {
        let output = """
        /dev/ttys001\tdemo--worker-1
        /dev/ttys010\tdemo--worker-10
        /dev/ttys011\tdemo--worker-1
        """
        XCTAssertEqual(
            TerminalLauncher.parseTmuxClientTTYs(output, session: "demo--worker-1"),
            ["/dev/ttys001", "/dev/ttys011"]
        )
    }
}
