import Darwin
import Foundation

let conductorSnapshotSchemaVersion = 4

enum ConductorError: LocalizedError {
    case executableMissing
    case commandFailed(String)
    case invalidResponse(String)
    case bundledCLIMissing
    case incompatibleVersion(String)

    var errorDescription: String? {
        switch self {
        case .executableMissing:
            return "Conductor CLI was not found. Install it from the app setup screen."
        case let .commandFailed(message), let .invalidResponse(message):
            return message
        case .bundledCLIMissing:
            return "This development build does not contain the bundled Conductor CLI."
        case let .incompatibleVersion(message):
            return message
        }
    }
}

private struct SemanticVersion: Comparable {
    let major: Int
    let minor: Int
    let patch: Int
    let prerelease: String?

    init?(_ output: String) {
        guard let token = output.split(whereSeparator: \.isWhitespace).last else { return nil }
        let parts = token.split(separator: "-", maxSplits: 1, omittingEmptySubsequences: false)
        let numbers = parts[0].split(separator: ".", omittingEmptySubsequences: false)
        guard numbers.count == 3,
              let major = Int(numbers[0]), let minor = Int(numbers[1]), let patch = Int(numbers[2]) else { return nil }
        self.major = major
        self.minor = minor
        self.patch = patch
        prerelease = parts.count == 2 ? String(parts[1]) : nil
    }

    static func < (left: Self, right: Self) -> Bool {
        if left.major != right.major { return left.major < right.major }
        if left.minor != right.minor { return left.minor < right.minor }
        if left.patch != right.patch { return left.patch < right.patch }
        switch (left.prerelease, right.prerelease) {
        case (.some, .none): return true
        case (.none, .some): return false
        case let (.some(left), .some(right)): return left < right
        case (.none, .none): return false
        }
    }
}

func compareConductorVersions(_ left: String, _ right: String) -> ComparisonResult? {
    guard let left = SemanticVersion(left), let right = SemanticVersion(right) else { return nil }
    if left < right { return .orderedAscending }
    if right < left { return .orderedDescending }
    return .orderedSame
}

@MainActor
final class RefreshCoalescer {
    private var running = false
    private var pending = false

    func run(_ operation: () async -> Void) async {
        if running {
            pending = true
            while running { await Task.yield() }
            return
        }
        running = true
        repeat {
            pending = false
            await operation()
        } while pending
        running = false
    }
}

@MainActor
func refreshedBrainSetupPrompt(
    refresh: () async -> Bool,
    prompt: () -> String
) async -> String? {
    guard await refresh() else { return nil }
    return prompt()
}

@MainActor
func runPeriodicRefresh(
    sleep: () async throws -> Void,
    refresh: () async -> Void
) async {
    while !Task.isCancelled {
        do { try await sleep() } catch { return }
        guard !Task.isCancelled else { return }
        await refresh()
    }
}

struct ConductorCLI {
    let executableURL: URL

    static var bundledURL: URL? {
        if let url = Bundle.main.url(forResource: "conductor", withExtension: nil) {
            return url
        }
        let development = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
            .appendingPathComponent("dist/conductor-darwin-arm64")
        return FileManager.default.isExecutableFile(atPath: development.path) ? development : nil
    }

    static var installedURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".local/bin/conductor")
    }

    static func snapshotCLI() throws -> ConductorCLI {
        guard let url = preferredAppCommandURL(
            bundled: bundledURL,
            installed: installedURL,
            installedIsExecutable: FileManager.default.isExecutableFile(atPath: installedURL.path)
        ) else {
            throw ConductorError.executableMissing
        }
        return ConductorCLI(executableURL: url)
    }

    static func commandCLI() throws -> ConductorCLI {
        // App operations stay paired with the bundled snapshot schema. The
        // installed binary is only a fallback for unbundled development runs.
        return try snapshotCLI()
    }

    func run(_ arguments: [String], stdin: String? = nil, allowFailure: Bool = false) async throws -> CommandResult {
        try await Task.detached(priority: .userInitiated) {
            let manager = FileManager.default
            let outputDirectory = manager.temporaryDirectory
                .appendingPathComponent("conductor-app-\(UUID().uuidString)", isDirectory: true)
            try manager.createDirectory(at: outputDirectory, withIntermediateDirectories: true)
            defer { try? manager.removeItem(at: outputDirectory) }

            let stdoutURL = outputDirectory.appendingPathComponent("stdout")
            let stderrURL = outputDirectory.appendingPathComponent("stderr")
            manager.createFile(atPath: stdoutURL.path, contents: nil, attributes: [.posixPermissions: 0o600])
            manager.createFile(atPath: stderrURL.path, contents: nil, attributes: [.posixPermissions: 0o600])
            let stdoutHandle = try FileHandle(forWritingTo: stdoutURL)
            let stderrHandle = try FileHandle(forWritingTo: stderrURL)
            defer {
                try? stdoutHandle.close()
                try? stderrHandle.close()
            }

            let process = Process()
            process.executableURL = executableURL
            process.arguments = arguments
            process.standardOutput = stdoutHandle
            process.standardError = stderrHandle
            var environment = ProcessInfo.processInfo.environment
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            environment["PATH"] = "\(home)/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
            process.environment = environment

            let inputPipe: Pipe?
            if stdin != nil {
                let pipe = Pipe()
                process.standardInput = pipe
                inputPipe = pipe
            } else {
                inputPipe = nil
            }

            try process.run()
            if let stdin, let inputPipe {
                inputPipe.fileHandleForWriting.write(Data(stdin.utf8))
                try inputPipe.fileHandleForWriting.close()
            }
            process.waitUntilExit()
            try stdoutHandle.synchronize()
            try stderrHandle.synchronize()

            let stdout = String(decoding: try Data(contentsOf: stdoutURL), as: UTF8.self)
            let stderr = String(decoding: try Data(contentsOf: stderrURL), as: UTF8.self)
            let result = CommandResult(stdout: stdout, stderr: stderr, exitCode: process.terminationStatus)
            if result.exitCode != 0 && !allowFailure {
                let message = stderr.trimmingCharacters(in: .whitespacesAndNewlines)
                throw ConductorError.commandFailed(message.isEmpty ? "Conductor exited with status \(result.exitCode)." : message)
            }
            return result
        }.value
    }
}

func preferredAppCommandURL(bundled: URL?, installed: URL, installedIsExecutable: Bool) -> URL? {
    bundled ?? (installedIsExecutable ? installed : nil)
}

func preferredDoctorURL(bundled: URL?, installed: URL, installedCLICompatible: Bool) -> URL? {
    installedCLICompatible ? installed : bundled
}

func validateSnapshotSchema(_ version: Int) throws {
    guard version == conductorSnapshotSchemaVersion else {
        throw ConductorError.invalidResponse(
            "Unsupported Conductor snapshot schema \(version); expected \(conductorSnapshotSchemaVersion)."
        )
    }
}

func conductorDataHome(
    environment: [String: String] = ProcessInfo.processInfo.environment,
    userHome: URL = FileManager.default.homeDirectoryForCurrentUser
) -> URL {
    if let configured = environment["CONDUCTOR_HOME"], !configured.isEmpty {
        return URL(fileURLWithPath: configured, isDirectory: true)
    }
    return userHome.appendingPathComponent(".conductor", isDirectory: true)
}

func legacyRuntimeFile(in home: URL, fileManager: FileManager = .default) -> URL? {
    var candidates = [
        home.appendingPathComponent("config.json"),
        home.appendingPathComponent("state.json")
    ]
    let projects = home.appendingPathComponent("projects", isDirectory: true)
    if let projectHomes = try? fileManager.contentsOfDirectory(
        at: projects,
        includingPropertiesForKeys: [.isDirectoryKey],
        options: [.skipsHiddenFiles]
    ) {
        candidates += projectHomes.map { $0.appendingPathComponent("state.json") }
    }
    return candidates.first { url in
        guard let data = try? Data(contentsOf: url),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let version = object["version"] as? NSNumber else { return false }
        return version.intValue == 1
    }
}

@MainActor
final class DashboardModel: ObservableObject {
    @Published private(set) var snapshot: ConductorSnapshot?
    @Published var selectedProjectID: String?
    @Published var selectedWorkerSession: String?
    @Published var selectedTaskID: String?
    @Published var inspectorSection: InspectorSection = .goal
    @Published private(set) var doctorReport: DoctorReport?
    @Published private(set) var doctorProjectID: String?
    @Published private(set) var modelCatalog: CodexModelCatalog?
    @Published var brainLaunchRequest: ProjectActionTarget?
    @Published private(set) var isRefreshing = false
    @Published var lastError: String?
    @Published var notice: String?
    @Published var setupNeeded = true
    @Published private(set) var installedCLICompatible = false

    private let watcher = DirectoryWatcher()
    private let refreshCoalescer = RefreshCoalescer()
    private var refreshDebounce: Task<Void, Never>?
    private var sessionMonitor: Task<Void, Never>?
    private var started = false
    private var lastRefreshSucceeded = false

    var selectedProject: ProjectSnapshot? {
        guard let selectedProjectID else { return snapshot?.projects.first }
        return snapshot?.projects.first { $0.id == selectedProjectID }
    }

    var selectedWorker: WorkerSummary? {
        guard let selectedProject, let selectedWorkerSession else { return nil }
        return selectedProject.workers(
            connectedSessions: Set(snapshot?.tmuxSessions ?? []),
            sessionActivity: snapshot?.sessionActivity ?? [:],
            sessionAttention: snapshot?.sessionAttention ?? [:]
        )
            .first { $0.session == selectedWorkerSession }
    }

    var selectedTask: ConductorTask? {
        guard let selectedProject else { return nil }
        if let selectedTaskID, let task = selectedProject.state.tasks[selectedTaskID] {
            return task
        }
        return selectedWorker?.activeTask ?? selectedProject.orderedTasks.first
    }

    func start() async {
        if started {
            await refresh()
            return
        }
        started = true
        startSessionMonitor()
        await refresh()
        await checkSetup()
    }

    private func startSessionMonitor() {
        sessionMonitor = Task { [weak self] in
            await runPeriodicRefresh(
                sleep: { try await Task.sleep(for: .seconds(3)) },
                refresh: { [weak self] in await self?.probeSessions() }
            )
        }
    }

    private func probeSessions() async {
        do {
            let cli = try ConductorCLI.snapshotCLI()
            let result = try await cli.run(["gui", "sessions"])
            guard let data = result.stdout.data(using: .utf8) else {
                throw ConductorError.invalidResponse("Conductor returned unreadable session data.")
            }
            let probe = try conductorDecoder().decode(TmuxSessionSnapshot.self, from: data)
            try validateSnapshotSchema(probe.schemaVersion)
            if snapshot == nil || snapshot?.tmuxSessions != probe.tmuxSessions || snapshot?.sessionActivity != probe.sessionActivity || snapshot?.sessionAttention != probe.sessionAttention || snapshot?.tmuxError != probe.tmuxError {
                await refresh()
            }
        } catch {
            // The full snapshot and manual refresh surface persistent errors;
            // a transient background probe must not interrupt the user.
        }
    }

    func refresh() async {
        await refreshCoalescer.run { [weak self] in
            guard let self else { return }
            self.lastRefreshSucceeded = await self.performRefresh()
        }
    }

    func refreshForAction() async -> Bool {
        await refresh()
        return lastRefreshSucceeded
    }

    func loadModelCatalog() async {
        do {
            let cli = try ConductorCLI.snapshotCLI()
            let result = try await cli.run(["gui", "models"])
            guard let data = result.stdout.data(using: .utf8) else { return }
            let catalog = try conductorDecoder().decode(CodexModelCatalog.self, from: data)
            try validateSnapshotSchema(catalog.schemaVersion)
            modelCatalog = catalog
        } catch {
            // Model entry remains free-form when catalog discovery is unavailable.
        }
    }

    private func performRefresh() async -> Bool {
        isRefreshing = true
        defer { isRefreshing = false }
        do {
            let cli = try ConductorCLI.snapshotCLI()
            let result = try await cli.run(["gui", "snapshot"])
            guard let data = result.stdout.data(using: .utf8) else {
                throw ConductorError.invalidResponse("Conductor returned unreadable snapshot data.")
            }
            let decoded = try conductorDecoder().decode(ConductorSnapshot.self, from: data)
            try validateSnapshotSchema(decoded.schemaVersion)
            snapshot = decoded
            if selectedProjectID == nil || !decoded.projects.contains(where: { $0.id == selectedProjectID }) {
                selectedProjectID = decoded.projects.first?.id
            }
            updateWatchers(decoded)
            lastError = nil
            return true
        } catch {
            lastError = error.localizedDescription
            return false
        }
    }

    func checkSetup() async {
        guard let bundled = ConductorCLI.bundledURL else {
            installedCLICompatible = FileManager.default.isExecutableFile(atPath: ConductorCLI.installedURL.path)
            setupNeeded = !installedCLICompatible
            return
        }
        do {
            let bundledVersion = try await ConductorCLI(executableURL: bundled).run(["version"]).stdout
            let installedCLI = ConductorCLI(executableURL: ConductorCLI.installedURL)
            let installedVersion = try await installedCLI.run(["version"]).stdout
            guard let comparison = compareConductorVersions(installedVersion, bundledVersion) else {
                installedCLICompatible = false
                setupNeeded = true
                return
            }
            guard comparison != .orderedAscending else {
                installedCLICompatible = false
                setupNeeded = true
                return
            }
            installedCLICompatible = true
            let result = try await installedCLI.run(["doctor", "--json"], allowFailure: true)
            guard let data = result.stdout.data(using: .utf8) else {
                setupNeeded = true
                return
            }
            let report = try conductorDecoder().decode(DoctorReport.self, from: data)
            setupNeeded = report.checks.first { $0.name == "hooks" }?.ok != true
        } catch {
            installedCLICompatible = false
            setupNeeded = true
        }
    }

    func installCLIAndHooks() async {
        do {
            lastError = nil
            guard let bundled = ConductorCLI.bundledURL else { throw ConductorError.bundledCLIMissing }
            let dataHome = conductorDataHome()
            if let legacy = legacyRuntimeFile(in: dataHome) {
                throw ConductorError.incompatibleVersion(
                    "Conductor 0.3 cannot use version 1 runtime data at \(legacy.path). The installed CLI was left unchanged. Move \(dataHome.path) aside, then try again."
                )
            }
            let target = ConductorCLI.installedURL
            let directory = target.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            let bundledVersion = try await ConductorCLI(executableURL: bundled).run(["version"]).stdout
            var keptNewerCLI = false
            if FileManager.default.isExecutableFile(atPath: target.path) {
                let installedVersion = try await ConductorCLI(executableURL: target).run(["version"]).stdout
                guard let comparison = compareConductorVersions(installedVersion, bundledVersion) else {
                    throw ConductorError.incompatibleVersion("The installed CLI version could not be compared safely. It was left unchanged.")
                }
                keptNewerCLI = comparison == .orderedDescending
                if comparison == .orderedAscending {
                    try await installBundledCLI(bundled, at: target, in: directory)
                }
            } else {
                try await installBundledCLI(bundled, at: target, in: directory)
            }
            let cli = ConductorCLI(executableURL: target)
            _ = try await cli.run(["init"])
            _ = try await cli.run(["hooks", "install"])
            installedCLICompatible = true
            setupNeeded = false
            notice = keptNewerCLI
                ? "The newer installed CLI was kept and Codex hooks were installed. Review them from /hooks in Codex."
                : "CLI and Codex hooks installed. Review and trust them from /hooks in Codex."
            await refresh()
            await loadDoctor()
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func installBundledCLI(_ bundled: URL, at target: URL, in directory: URL) async throws {
        let temporary = directory.appendingPathComponent(".conductor-\(UUID().uuidString).tmp")
        defer { try? FileManager.default.removeItem(at: temporary) }
        try FileManager.default.copyItem(at: bundled, to: temporary)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: temporary.path)
        _ = try await ConductorCLI(executableURL: temporary).run(["version"])
        guard rename(temporary.path, target.path) == 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
    }

    func sendGoal(projectID: String, worker: String, objective: String) async {
        await execute(projectID: projectID, arguments: ["goal", worker, "--stdin"], stdin: objective, success: "Goal sent to \(worker).")
    }

    func finish(projectID: String, worker: String, taskID: String, message: String, status: String) async {
        var arguments = ["finish", worker, "--task-id", taskID, "--status", status]
        let input: String?
        if message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            input = nil
        } else {
            arguments.append("--stdin")
            input = message
        }
        await execute(projectID: projectID, arguments: arguments, stdin: input, success: "\(worker) finished manually.")
    }

    func flush(projectID: String, force: Bool) async {
        await execute(projectID: projectID, arguments: force ? ["flush", "--force"] : ["flush"], success: "Oldest handoff delivered or queue already empty.")
    }

    func markIdle(projectID: String) async {
        await execute(projectID: projectID, arguments: ["idle"], success: "Brain marked idle.")
    }

    func sendBrainSetup(projectID: String, prompt: String) async {
        await execute(
            projectID: projectID,
            arguments: ["brain", "setup", "--stdin"],
            stdin: prompt,
            success: "Setup prompt sent to Brain."
        )
    }

    func initializeProject(_ name: String) async {
        do {
            lastError = nil
            let cli = try ConductorCLI.commandCLI()
            _ = try await cli.run(["project", "init", name])
            notice = "Project \(name) initialized."
            await refresh()
            selectedProjectID = name.lowercased()
        } catch {
            lastError = error.localizedDescription
        }
    }

    func deleteProject(_ name: String) async {
        do {
            lastError = nil
            let cli = try ConductorCLI.commandCLI()
            _ = try await cli.run(["project", "delete", name, "--yes"])
            if selectedProjectID == name {
                selectedProjectID = nil
                selectedWorkerSession = nil
                selectedTaskID = nil
            }
            notice = "Project \(name) deleted from Conductor. Workspaces and terminal sessions were not changed."
            await refresh()
        } catch {
            lastError = error.localizedDescription
        }
    }

    func installHooks() async {
        guard installedCLICompatible, FileManager.default.isExecutableFile(atPath: ConductorCLI.installedURL.path) else {
            lastError = "Install or update the CLI from the setup sheet before installing persistent hooks."
            return
        }
        do {
            lastError = nil
            let cli = ConductorCLI(executableURL: ConductorCLI.installedURL)
            _ = try await cli.run(["hooks", "install"])
            notice = "Codex hooks installed. Review and trust them from /hooks."
            await loadDoctor()
        } catch {
            lastError = error.localizedDescription
        }
    }

    func uninstallHooks() async {
        await execute(projectID: nil, arguments: ["hooks", "uninstall"], success: "Conductor hooks removed.")
        await loadDoctor()
    }

    func loadDoctor() async {
        do {
            lastError = nil
            guard let doctorURL = preferredDoctorURL(
                bundled: ConductorCLI.bundledURL,
                installed: ConductorCLI.installedURL,
                installedCLICompatible: installedCLICompatible
            ) else { throw ConductorError.executableMissing }
            // Hook health must be evaluated by the installed executable because
            // handlers intentionally reference ~/.local/bin/conductor.
            let cli = ConductorCLI(executableURL: doctorURL)
            let projectID = selectedProjectID
            let projectPrefix = projectID.map { ["--project", $0] } ?? []
            let result = try await cli.run(projectPrefix + ["doctor", "--json"], allowFailure: true)
            guard let data = result.stdout.data(using: .utf8) else { throw ConductorError.invalidResponse("Doctor returned unreadable data.") }
            doctorReport = try conductorDecoder().decode(DoctorReport.self, from: data)
            doctorProjectID = projectID
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func execute(projectID: String?, arguments: [String], stdin: String? = nil, success: String) async {
        do {
            lastError = nil
            let cli = try ConductorCLI.commandCLI()
            let prefix = projectID.map { ["--project", $0] } ?? []
            _ = try await cli.run(prefix + arguments, stdin: stdin)
            notice = success
            await refresh()
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func updateWatchers(_ snapshot: ConductorSnapshot) {
        watcher.watch(paths: conductorWatchPaths(snapshot)) { [weak self] in
            Task { @MainActor in
                self?.scheduleRefresh()
            }
        }
    }

    private func scheduleRefresh() {
        refreshDebounce?.cancel()
        refreshDebounce = Task {
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            await refresh()
        }
    }
}

func conductorWatchPaths(_ snapshot: ConductorSnapshot) -> [String] {
    var paths = Set([snapshot.conductorHome])
    for project in snapshot.projects {
        paths.insert(URL(fileURLWithPath: project.statePath).deletingLastPathComponent().path)
        let log = URL(fileURLWithPath: project.logPath)
        paths.insert(log.path)
        paths.insert(log.deletingLastPathComponent().path)
    }
    return paths.sorted()
}

final class DirectoryWatcher {
    private var sources: [DispatchSourceFileSystemObject] = []
    private var descriptors: [Int32] = []

    deinit { stop() }

    func watch(paths: [String], onChange: @escaping () -> Void) {
        stop()
        for path in paths {
            let descriptor = open(path, O_EVTONLY)
            guard descriptor >= 0 else { continue }
            let source = DispatchSource.makeFileSystemObjectSource(
                fileDescriptor: descriptor,
                eventMask: [.write, .extend, .attrib, .rename, .delete],
                queue: .main
            )
            source.setEventHandler(handler: onChange)
            source.setCancelHandler { close(descriptor) }
            descriptors.append(descriptor)
            sources.append(source)
            source.resume()
        }
    }

    func stop() {
        for source in sources { source.cancel() }
        sources.removeAll()
        descriptors.removeAll()
    }
}
