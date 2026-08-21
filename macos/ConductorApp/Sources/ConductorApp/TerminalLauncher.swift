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

    var isRunning: Bool {
        !NSRunningApplication.runningApplications(withBundleIdentifier: bundleIdentifier).isEmpty
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
        _ = try await runAppleScript(launchAppleScript(terminal: terminal, command: command, session: session))
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
        _ = try await runAppleScript(
            launchAppleScript(
                terminal: terminal,
                command: attachTerminalCommand(session: session, workspace: workspace, tmuxExecutable: tmuxExecutable),
                session: session
            )
        )
    }

    static func focus(session: String, tmuxExecutable: String) async throws {
        let running = configuredPriority.filter(\.isRunning)
        guard !running.isEmpty else {
            throw ConductorError.commandFailed("No supported terminal is currently open.")
        }
        let clientTTYs = await tmuxClientTTYs(session: session, tmuxExecutable: tmuxExecutable)
        var lastError: Error?
        for terminal in running {
            do {
                let result = try await runAppleScript(
                    focusAppleScript(terminal: terminal, session: session, clientTTYs: clientTTYs)
                )
                if result.trimmingCharacters(in: .whitespacesAndNewlines) == "focused" {
                    return
                }
            } catch {
                lastError = error
            }
        }
        if let lastError { throw lastError }
        throw ConductorError.commandFailed("No open terminal was found for tmux session \(session). Nothing was opened.")
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

    static func launchAppleScript(terminal: TerminalKind, command: String, session: String) -> String {
        let escapedCommand = appleScriptQuote(applicationCommand(for: terminal, shellCommand: command))
        let title = appleScriptQuote("Conductor · \(session)")
        switch terminal {
        case .terminal:
            return """
            tell application id "com.apple.Terminal"
                activate
                set targetTab to do script "\(escapedCommand)"
                try
                    set custom title of targetTab to "\(title)"
                end try
            end tell
            """
        case .iterm:
            return """
            tell application id "com.googlecode.iterm2"
                activate
                set targetWindow to create window with default profile command "\(escapedCommand)"
                try
                    set name of current session of targetWindow to "\(title)"
                end try
            end tell
            """
        }
    }

    static func focusAppleScript(terminal: TerminalKind, session: String, clientTTYs: [String] = []) -> String {
        let title = appleScriptQuote("Conductor · \(session)")
        let ttys = clientTTYs.map { "\"\(appleScriptQuote($0))\"" }.joined(separator: ", ")
        let targetTTYs = "{\(ttys)}"
        switch terminal {
        case .terminal:
            return """
            tell application id "com.apple.Terminal"
                set targetTTYs to \(targetTTYs)
                repeat with terminalWindow in windows
                    repeat with terminalTab in tabs of terminalWindow
                        try
                            if custom title of terminalTab is "\(title)" or targetTTYs contains (tty of terminalTab) then
                                set selected tab of terminalWindow to terminalTab
                                set index of terminalWindow to 1
                                activate
                                return "focused"
                            end if
                        end try
                    end repeat
                end repeat
            end tell
            return "not-found"
            """
        case .iterm:
            return """
            tell application id "com.googlecode.iterm2"
                set targetTTYs to \(targetTTYs)
                repeat with terminalWindow in windows
                    repeat with terminalTab in tabs of terminalWindow
                        repeat with terminalSession in sessions of terminalTab
                            try
                                if name of terminalSession is "\(title)" or targetTTYs contains (tty of terminalSession) then
                                    select terminalWindow
                                    select terminalTab
                                    select terminalSession
                                    activate
                                    return "focused"
                                end if
                            end try
                        end repeat
                    end repeat
                end repeat
            end tell
            return "not-found"
            """
        }
    }

    static func parseTmuxClientTTYs(_ output: String, session: String) -> [String] {
        output.split(whereSeparator: \.isNewline).compactMap { row in
            let fields = row.split(separator: "\t", maxSplits: 1, omittingEmptySubsequences: false)
            guard fields.count == 2, String(fields[1]) == session else { return nil }
            let tty = String(fields[0]).trimmingCharacters(in: .whitespacesAndNewlines)
            return tty.isEmpty ? nil : tty
        }
    }

    private static func tmuxClientTTYs(session: String, tmuxExecutable: String) async -> [String] {
        await Task.detached(priority: .userInitiated) {
            let process = Process()
            let outputPipe = Pipe()
            if tmuxExecutable.hasPrefix("/") {
                process.executableURL = URL(fileURLWithPath: tmuxExecutable)
                process.arguments = ["list-clients", "-F", "#{client_tty}\t#{session_name}"]
            } else {
                process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
                process.arguments = [tmuxExecutable, "list-clients", "-F", "#{client_tty}\t#{session_name}"]
            }
            process.standardOutput = outputPipe
            process.standardError = FileHandle.nullDevice
            do {
                try process.run()
                process.waitUntilExit()
                guard process.terminationStatus == 0 else { return [] }
                let data = outputPipe.fileHandleForReading.readDataToEndOfFile()
                return parseTmuxClientTTYs(String(decoding: data, as: UTF8.self), session: session)
            } catch {
                return []
            }
        }.value
    }

    private static func runAppleScript(_ script: String) async throws -> String {
        try await Task.detached(priority: .userInitiated) {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
            process.arguments = ["-e", script]
            let outputPipe = Pipe()
            let errorPipe = Pipe()
            process.standardOutput = outputPipe
            process.standardError = errorPipe
            try process.run()
            process.waitUntilExit()
            if process.terminationStatus != 0 {
                let message = String(decoding: errorPipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                throw ConductorError.commandFailed(message.isEmpty ? "The terminal could not be opened." : message)
            }
            return String(decoding: outputPipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
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
