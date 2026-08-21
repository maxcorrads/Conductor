import XCTest
@testable import ConductorApp

final class ModelsTests: XCTestCase {
    func testWorkerNumberSortsAliases() {
        XCTAssertEqual(workerNumber("worker-2"), 2)
        XCTAssertEqual(workerNumber("worker-12"), 12)
        XCTAssertEqual(workerNumber("brain"), .max)
    }

    func testDecodesMinimalSnapshot() throws {
        let json = #"""
        {
          "schema_version": 2,
          "conductor_version": "0.3.0",
          "generated_at": "2026-08-21T12:00:00Z",
          "conductor_home": "/tmp/conductor",
          "executable": "/tmp/conductor/bin",
          "tmux_executable": "/opt/homebrew/bin/tmux",
          "tmux_sessions": [],
          "session_activity": {},
          "projects": []
        }
        """#
        let data = Data(json.utf8)
        let snapshot = try conductorDecoder().decode(ConductorSnapshot.self, from: data)
        XCTAssertEqual(snapshot.schemaVersion, 2)
        XCTAssertEqual(snapshot.conductorVersion, "0.3.0")
    }

    func testDecodesLightweightSessionProbe() throws {
        let json = #"{"schema_version":2,"generated_at":"2026-08-21T12:00:00Z","tmux_executable":"/opt/homebrew/bin/tmux","tmux_sessions":["brain","worker-1"],"session_activity":{"brain":true}}"#
        let probe = try conductorDecoder().decode(TmuxSessionSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(probe.tmuxSessions, ["brain", "worker-1"])
        XCTAssertEqual(probe.sessionActivity["brain"], true)
        XCTAssertNil(probe.tmuxError)
    }

    func testRejectsLegacySnapshotSchema() {
        XCTAssertThrowsError(try validateSnapshotSchema(1))
        XCTAssertNoThrow(try validateSnapshotSchema(2))
    }

    func testLegacyRuntimePreflightFindsRootAndProjectState() throws {
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("conductor-preflight-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: home) }
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        let config = home.appendingPathComponent("config.json")
        try Data(#"{"version":2}"#.utf8).write(to: config)
        XCTAssertNil(legacyRuntimeFile(in: home))

        let project = home.appendingPathComponent("projects/demo", isDirectory: true)
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        let state = project.appendingPathComponent("state.json")
        try Data(#"{"version":1}"#.utf8).write(to: state)
        XCTAssertEqual(
            legacyRuntimeFile(in: home)?.standardizedFileURL.resolvingSymlinksInPath(),
            state.standardizedFileURL.resolvingSymlinksInPath()
        )
    }

    func testConductorDataHomeHonorsEnvironment() {
        XCTAssertEqual(
            conductorDataHome(environment: ["CONDUCTOR_HOME": "/tmp/custom-conductor"]),
            URL(fileURLWithPath: "/tmp/custom-conductor", isDirectory: true)
        )
    }

    func testDecodesModelCatalogWithModelSpecificEfforts() throws {
        let json = #"{"schema_version":2,"codex_executable":"/usr/local/bin/codex","models":[{"slug":"custom","display_name":"Custom","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high","description":"Deep"},{"effort":"ultra","description":"Delegated"}]}]}"#
        let catalog = try conductorDecoder().decode(CodexModelCatalog.self, from: Data(json.utf8))
        XCTAssertEqual(catalog.models.first?.slug, "custom")
        XCTAssertEqual(catalog.models.first?.supportedReasoningLevels.map(\.effort), ["high", "ultra"])
    }

    func testDecodesProjectWithEmptyCollections() throws {
        let json = #"""
        {
          "schema_version": 2,
          "conductor_version": "0.3.0",
          "generated_at": "2026-08-21T12:00:00Z",
          "conductor_home": "/tmp/conductor",
          "executable": "/tmp/conductor/bin",
          "tmux_executable": "/opt/homebrew/bin/tmux",
          "tmux_sessions": [],
          "session_activity": {},
          "projects": [{
            "id": "empty",
            "brain_session": "empty--brain",
            "state_path": "/tmp/conductor/state.json",
            "log_path": "/tmp/conductor/log",
            "worker_sessions": [],
            "worker_session_template": "empty--worker-%d",
            "task_count": 0,
            "handoff_count": 0,
            "history_truncated": false,
            "state": {
              "version": 2,
              "project_id": "empty",
              "brain": {"session": "empty--brain", "busy": false},
              "workers": {},
              "tasks": {},
              "deliveries": {}
            },
            "task_order": [],
            "handoff_order": [],
            "goal_texts": {},
            "goal_text_truncated": {"task": true},
            "handoff_messages": {},
            "handoff_message_truncated": {"handoff": true},
            "log_tail": ""
          }]
        }
        """#
        let snapshot = try conductorDecoder().decode(ConductorSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(snapshot.projects.first?.id, "empty")
        XCTAssertEqual(snapshot.projects.first?.taskOrder, [])
        XCTAssertEqual(snapshot.projects.first?.handoffOrder, [])
        XCTAssertEqual(snapshot.projects.first?.goalTextTruncated["task"], true)
        XCTAssertEqual(snapshot.projects.first?.handoffMessageTruncated["handoff"], true)
    }

    func testProjectIDNormalizationAndValidation() {
        XCTAssertEqual(normalizedProjectID("  My   Project  "), "my-project")
        XCTAssertTrue(isValidProjectID("My Project"))
        XCTAssertTrue(isValidProjectID("release-3"))
        XCTAssertFalse(isValidProjectID("-invalid"))
        XCTAssertFalse(isValidProjectID("contains/slash"))
        XCTAssertFalse(isValidProjectID("a--b"))
    }

    func testZeroTimestampIsNotPresentedAsARealDate() {
        XCTAssertEqual(formattedTimestamp("0001-01-01T00:00:00Z"), "—")
        XCTAssertEqual(formattedTimestamp(nil), "—")
    }

    func testTerminalCommandUsesResolvedTmuxAndSurvivesMissingWorkspace() {
        let command = TerminalLauncher.attachTerminalCommand(
            session: "demo--worker-1",
            workspace: "/tmp/old worktree",
            tmuxExecutable: "/opt/homebrew/bin/tmux"
        )
        XCTAssertTrue(command.contains("if [ -d '/tmp/old worktree' ]; then cd -- '/tmp/old worktree'; fi;"))
        XCTAssertTrue(command.hasSuffix("exec '/opt/homebrew/bin/tmux' attach-session -t 'demo--worker-1'"))
        XCTAssertFalse(command.contains("&&"))
    }

    func testTerminalCommandCreatesCodexWithSelectedModelAndEffort() {
        let options = CodexLaunchOptions(model: "custom model'; touch /tmp/no", reasoningEffort: "max")
        let command = TerminalLauncher.terminalCommand(
            session: "demo--worker-1",
            workspace: "/tmp/work tree",
            tmuxExecutable: "/opt/homebrew/bin/tmux",
            codexOptions: options
        )
        let inner = "exec " + [
            "codex", "--model", options.model, "--config", "model_reasoning_effort=max"
        ].map(shellQuote).joined(separator: " ")
        let sessionCommand = "exec /bin/zsh -lic \(shellQuote(inner))"
        XCTAssertTrue(command.contains("new-session -A -s 'demo--worker-1'"))
        XCTAssertEqual(TerminalLauncher.codexSessionCommand(options), sessionCommand)
        XCTAssertTrue(command.hasSuffix(shellQuote(sessionCommand)))
    }

    func testITermCommandExplicitlyInvokesShell() {
        let shellCommand = "if [ -d '/tmp/work tree' ]; then cd -- '/tmp/work tree'; fi; exec '/opt/homebrew/bin/tmux' new-session -A -s 'demo--worker-1'"
        let command = TerminalLauncher.applicationCommand(for: .iterm, shellCommand: shellCommand)
        XCTAssertTrue(command.hasPrefix("/bin/sh -c "))
        XCTAssertEqual(command, "/bin/sh -c \(shellQuote(shellCommand))")
        XCTAssertEqual(TerminalLauncher.applicationCommand(for: .terminal, shellCommand: shellCommand), shellCommand)
    }

    func testVersionComparisonPreventsDowngrades() {
        XCTAssertEqual(compareConductorVersions("conductor 0.2.0", "conductor 0.3.0"), .orderedAscending)
        XCTAssertEqual(compareConductorVersions("conductor 0.3.0", "conductor 0.3.0"), .orderedSame)
        XCTAssertEqual(compareConductorVersions("conductor 0.4.0", "conductor 0.3.0"), .orderedDescending)
        XCTAssertEqual(compareConductorVersions("conductor 0.3.0-beta.1", "conductor 0.3.0"), .orderedAscending)
    }

    func testBundledCLIAlwaysWinsForAppCommands() {
        let bundled = URL(fileURLWithPath: "/Applications/Conductor.app/Contents/Resources/conductor")
        let installed = URL(fileURLWithPath: "/Users/test/.local/bin/conductor")
        XCTAssertEqual(preferredAppCommandURL(bundled: bundled, installed: installed, installedIsExecutable: true), bundled)
        XCTAssertEqual(preferredAppCommandURL(bundled: nil, installed: installed, installedIsExecutable: true), installed)
        XCTAssertNil(preferredAppCommandURL(bundled: nil, installed: installed, installedIsExecutable: false))
        XCTAssertEqual(preferredDoctorURL(bundled: bundled, installed: installed, installedCLICompatible: true), installed)
        XCTAssertEqual(preferredDoctorURL(bundled: bundled, installed: installed, installedCLICompatible: false), bundled)
    }

    @MainActor
    func testRefreshCoalescerRunsAgainWhenRequestArrivesDuringRefresh() async {
        let coalescer = RefreshCoalescer()
        let firstStarted = expectation(description: "first refresh started")
        var releaseFirst = false
        var runCount = 0

        let first = Task { @MainActor in
            await coalescer.run {
                runCount += 1
                if runCount == 1 {
                    firstStarted.fulfill()
                    while !releaseFirst { await Task.yield() }
                }
            }
        }

        await fulfillment(of: [firstStarted], timeout: 1)
        await coalescer.run { XCTFail("the concurrent request closure must be coalesced") }
        releaseFirst = true
        await first.value

        XCTAssertEqual(runCount, 2)
    }

    @MainActor
    func testPeriodicRefreshRunsForEachInjectedTick() async {
        var sleeps = 0
        var refreshes = 0
        await runPeriodicRefresh(
            sleep: {
                sleeps += 1
                if sleeps > 2 { throw CancellationError() }
            },
            refresh: { refreshes += 1 }
        )
        XCTAssertEqual(refreshes, 2)
    }


    func testWatchPathsIncludeLogFileAndDirectory() throws {
        let json = #"""
        {
          "schema_version": 2,
          "conductor_version": "0.3.0",
          "generated_at": "2026-08-21T12:00:00Z",
          "conductor_home": "/tmp/conductor",
          "executable": "/tmp/conductor/bin",
          "tmux_executable": "/opt/homebrew/bin/tmux",
          "tmux_sessions": [],
          "session_activity": {},
          "projects": [{
            "id": "empty",
            "brain_session": "empty--brain",
            "state_path": "/tmp/conductor/projects/empty/state.json",
            "log_path": "/tmp/conductor/projects/empty/logs/conductor.log",
            "worker_sessions": [],
            "worker_session_template": "empty--worker-%d",
            "task_count": 0,
            "handoff_count": 0,
            "history_truncated": false,
            "state": {
              "version": 2,
              "project_id": "empty",
              "brain": {"session": "empty--brain", "busy": false},
              "workers": {}, "tasks": {}, "deliveries": {}
            },
            "task_order": [], "handoff_order": [],
            "goal_texts": {}, "goal_text_truncated": {},
            "handoff_messages": {}, "handoff_message_truncated": {}, "log_tail": ""
          }]
        }
        """#
        let snapshot = try conductorDecoder().decode(ConductorSnapshot.self, from: Data(json.utf8))
        let paths = conductorWatchPaths(snapshot)
        XCTAssertTrue(paths.contains("/tmp/conductor/projects/empty/logs/conductor.log"))
        XCTAssertTrue(paths.contains("/tmp/conductor/projects/empty/logs"))
    }
}
