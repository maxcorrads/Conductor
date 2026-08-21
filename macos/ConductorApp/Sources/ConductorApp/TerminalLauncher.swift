import AppKit
import Foundation

enum TerminalKind: String, CaseIterable, Identifiable {
    case terminal = "Terminal"
    case iterm = "iTerm2"

    var id: String { rawValue }

    var bundleIdentifier: String {
        switch self {
        case .terminal: return "com.apple.Terminal"
        case .iterm: return "com.googlecode.iterm2"
        }
    }

    var iconName: String {
        switch self {
        case .terminal: return "apple.terminal"
        case .iterm: return "terminal"
        }
    }

    var isInstalled: Bool {
        NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleIdentifier) != nil
    }
}

enum TerminalLauncher {
    static let priorityKey = "terminalPriority"

    static var configuredPriority: [TerminalKind] {
        let saved = UserDefaults.standard.string(forKey: priorityKey) ?? "iTerm2,Terminal"
        let decoded = saved.split(separator: ",").compactMap { TerminalKind(rawValue: String($0)) }
        let missing = TerminalKind.allCases.filter { !decoded.contains($0) }
        return decoded + missing
    }

    static func savePriority(_ priority: [TerminalKind]) {
        UserDefaults.standard.set(priority.map(\.rawValue).joined(separator: ","), forKey: priorityKey)
    }

    static func launch(
        session: String,
        workspace: String?,
        tmuxExecutable: String,
        codexOptions: CodexLaunchOptions
    ) async throws {
        guard let terminal = configuredPriority.first(where: \.isInstalled) else {
            throw ConductorError.commandFailed("No supported terminal is installed. Enable Terminal or install iTerm2.")
        }
        let command = terminalCommand(
            session: session,
            workspace: workspace,
            tmuxExecutable: tmuxExecutable,
            codexOptions: codexOptions
        )
        try await runAppleScript(terminal: terminal, command: command)
    }

    static func terminalCommand(
        session: String,
        workspace: String?,
        tmuxExecutable: String,
        codexOptions: CodexLaunchOptions
    ) -> String {
        let directory = workspace?.trimmingCharacters(in: .whitespacesAndNewlines)
        let prefix = (directory?.isEmpty == false) ? "if [ -d \(shellQuote(directory!)) ]; then cd -- \(shellQuote(directory!)); fi; " : ""
        var command = prefix + "exec \(shellQuote(tmuxExecutable)) new-session -A -s \(shellQuote(session))"
        command += " \(shellQuote(codexSessionCommand(codexOptions)))"
        return command
    }

    static func attach(session: String, workspace: String?, tmuxExecutable: String) async throws {
        guard let terminal = configuredPriority.first(where: \.isInstalled) else {
            throw ConductorError.commandFailed("No supported terminal is installed. Enable Terminal or install iTerm2.")
        }
        try await runAppleScript(
            terminal: terminal,
            command: attachTerminalCommand(session: session, workspace: workspace, tmuxExecutable: tmuxExecutable)
        )
    }

    static func attachTerminalCommand(session: String, workspace: String?, tmuxExecutable: String) -> String {
        let directory = workspace?.trimmingCharacters(in: .whitespacesAndNewlines)
        let prefix = (directory?.isEmpty == false) ? "if [ -d \(shellQuote(directory!)) ]; then cd -- \(shellQuote(directory!)); fi; " : ""
        return prefix + "exec \(shellQuote(tmuxExecutable)) attach-session -t \(shellQuote(session))"
    }

    static func codexSessionCommand(_ options: CodexLaunchOptions) -> String {
        var arguments = ["codex"]
        let model = options.model.trimmingCharacters(in: .whitespacesAndNewlines)
        if !model.isEmpty {
            arguments += ["--model", model]
        }
        let effort = options.reasoningEffort.trimmingCharacters(in: .whitespacesAndNewlines)
        if !effort.isEmpty {
            arguments += ["--config", "model_reasoning_effort=\(effort)"]
        }
        let codexCommand = "exec " + arguments.map(shellQuote).joined(separator: " ")
        return "exec /bin/zsh -lic \(shellQuote(codexCommand))"
    }

    static func applicationCommand(for terminal: TerminalKind, shellCommand: String) -> String {
        switch terminal {
        case .terminal:
            return shellCommand
        case .iterm:
            return "/bin/sh -c \(shellQuote(shellCommand))"
        }
    }

    private static func runAppleScript(terminal: TerminalKind, command: String) async throws {
        try await Task.detached(priority: .userInitiated) {
            let escaped = appleScriptQuote(applicationCommand(for: terminal, shellCommand: command))
            let script: String
            switch terminal {
            case .terminal:
                script = """
                tell application id "com.apple.Terminal"
                    activate
                    do script "\(escaped)"
                end tell
                """
            case .iterm:
                script = """
                tell application id "com.googlecode.iterm2"
                    activate
                    create window with default profile command "\(escaped)"
                end tell
                """
            }
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
            process.arguments = ["-e", script]
            let errorPipe = Pipe()
            process.standardError = errorPipe
            try process.run()
            process.waitUntilExit()
            if process.terminationStatus != 0 {
                let message = String(decoding: errorPipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                throw ConductorError.commandFailed(message.isEmpty ? "The terminal could not be opened." : message)
            }
        }.value
    }
}

struct CodexLaunchOptions: Equatable {
    let model: String
    let reasoningEffort: String
}

func shellQuote(_ value: String) -> String {
    "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
}

func appleScriptQuote(_ value: String) -> String {
    value
        .replacingOccurrences(of: "\\", with: "\\\\")
        .replacingOccurrences(of: "\"", with: "\\\"")
}
