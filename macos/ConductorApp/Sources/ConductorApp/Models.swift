import Foundation

struct ConductorSnapshot: Decodable {
    let schemaVersion: Int
    let conductorVersion: String
    let generatedAt: String
    let conductorHome: String
    let executable: String
    let tmuxExecutable: String
    let tmuxSessions: [String]
    let sessionActivity: [String: Bool]
    let tmuxError: String?
    let projects: [ProjectSnapshot]
}

struct TmuxSessionSnapshot: Decodable {
    let schemaVersion: Int
    let generatedAt: String
    let tmuxExecutable: String
    let tmuxSessions: [String]
    let sessionActivity: [String: Bool]
    let tmuxError: String?
}

struct CodexModelCatalog: Decodable {
    let schemaVersion: Int
    let codexExecutable: String?
    let models: [CodexModelOption]
    let error: String?
}

struct CodexModelOption: Decodable, Identifiable, Hashable {
    var id: String { slug }
    let slug: String
    let displayName: String
    let defaultReasoningLevel: String
    let supportedReasoningLevels: [CodexReasoningOption]
}

struct CodexReasoningOption: Decodable, Identifiable, Hashable {
    var id: String { effort }
    let effort: String
    let description: String
}

struct ProjectSnapshot: Decodable, Identifiable {
    let id: String
    let brainSession: String
    let statePath: String
    let logPath: String
    let workerSessions: [String]
    let workerSessionTemplate: String
    let taskCount: Int
    let handoffCount: Int
    let historyTruncated: Bool
    let state: ConductorState
    let taskOrder: [String]
    let handoffOrder: [String]
    let goalTexts: [String: String]
    let goalTextTruncated: [String: Bool]
    let handoffMessages: [String: String]
    let handoffMessageTruncated: [String: Bool]
    let logTail: String

    var orderedTasks: [ConductorTask] {
        taskOrder.compactMap { state.tasks[$0] }
    }

    var orderedHandoffs: [Delivery] {
        handoffOrder.compactMap { state.deliveries[$0] }
    }

    var pendingHandoffs: [Delivery] {
        orderedHandoffs.filter { $0.status == "pending" }
    }

    func workers(connectedSessions: Set<String>, sessionActivity: [String: Bool] = [:]) -> [WorkerSummary] {
        let sessions = Set(workerSessions)
        return sessions.compactMap { session in
            guard let alias = workerAlias(for: session) else { return nil }
            let activeTask = orderedTasks.first { $0.workerSession == session && $0.status == "running" }
            let worker = state.workers[session]
            return WorkerSummary(
                id: session,
                alias: alias,
                session: session,
                connected: connectedSessions.contains(session),
                busy: sessionActivity[session] ?? (worker?.busy == true),
                workspace: worker?.cwd ?? activeTask?.workspace ?? "",
                activeTask: activeTask
            )
        }.sorted {
            let left = workerNumber($0.alias)
            let right = workerNumber($1.alias)
            return left == right ? $0.alias.localizedStandardCompare($1.alias) == .orderedAscending : left < right
        }
    }

    private func workerAlias(for session: String) -> String? {
        if id == "default" {
            return session
        }
        let prefix = "\(id)--"
        guard session.hasPrefix(prefix) else { return nil }
        let alias = String(session.dropFirst(prefix.count))
        guard alias.hasPrefix("worker-"), workerNumber(alias) < Int.max else { return nil }
        return alias
    }
}

struct ConductorState: Decodable {
    let version: Int
    let projectId: String?
    let brain: Activity
    let workers: [String: Worker]
    let tasks: [String: ConductorTask]
    let deliveries: [String: Delivery]
}

struct Activity: Decodable {
    let session: String
    let pane: String?
    let codexSessionId: String?
    let cwd: String?
    let busy: Bool
    let turnId: String?
    let reservedDelivery: String?
    let updatedAt: String?
}

struct Worker: Decodable {
    let session: String
    let pane: String?
    let codexSessionId: String?
    let cwd: String?
    let busy: Bool
    let updatedAt: String?
}

struct ConductorTask: Decodable, Identifiable, Hashable {
    let id: String
    let workerSession: String
    let workerAlias: String?
    let workerPane: String?
    let workspace: String
    let senderSession: String?
    let status: String
    let terminalGoalStatus: String?
    let objectivePath: String
    let sentGoalObjective: String
    let observedGoalStatus: String?
    let createdAt: String
    let updatedAt: String
    let finishedAt: String?
    let lastError: String?
}

struct Delivery: Decodable, Identifiable, Hashable {
    let id: String
    let taskId: String
    let workerSession: String
    let workerAlias: String?
    let workspace: String
    let goalStatus: String
    let messagePath: String
    let status: String
    let attempts: Int
    let lastError: String?
    let createdAt: String
    let deliveredAt: String?
}

struct WorkerSummary: Identifiable, Hashable {
    let id: String
    let alias: String
    let session: String
    let connected: Bool
    let busy: Bool
    let workspace: String
    let activeTask: ConductorTask?
}

struct DoctorReport: Decodable {
    let checks: [DoctorCheck]
    let criticalFailure: Bool
    let note: String
}

struct DoctorCheck: Decodable, Identifiable {
    var id: String { name }
    let name: String
    let value: String
    let ok: Bool
}

struct CommandResult {
    let stdout: String
    let stderr: String
    let exitCode: Int32
}

struct WorkerActionTarget: Identifiable {
    let projectID: String
    let worker: WorkerSummary
    var id: String { "\(projectID):\(worker.id)" }
}

struct ProjectActionTarget: Identifiable {
    let projectID: String
    var id: String { projectID }
}

enum InspectorSection: String, CaseIterable, Identifiable {
    case goal = "Goal"
    case inbox = "Inbox"
    case log = "Log"
    case health = "Health"

    var id: String { rawValue }
}

func workerNumber(_ alias: String) -> Int {
    guard let value = Int(alias.split(separator: "-").last ?? "") else { return .max }
    return value
}

func conductorDecoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return decoder
}

func formattedTimestamp(_ value: String?) -> String {
    guard let value, !value.isEmpty else { return "—" }
    guard !value.hasPrefix("0001-01-01T00:00:00") else { return "—" }
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let standard = ISO8601DateFormatter()
    standard.formatOptions = [.withInternetDateTime]
    guard let date = fractional.date(from: value) ?? standard.date(from: value) else { return value }
    return date.formatted(date: .abbreviated, time: .shortened)
}

func normalizedProjectID(_ value: String) -> String {
    value
        .trimmingCharacters(in: .whitespacesAndNewlines)
        .lowercased()
        .split(whereSeparator: \Character.isWhitespace)
        .joined(separator: "-")
}

func isValidProjectID(_ value: String) -> Bool {
    let normalized = normalizedProjectID(value)
    return !normalized.contains("--") && normalized.range(
        of: #"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$"#,
        options: .regularExpression
    ) != nil
}
