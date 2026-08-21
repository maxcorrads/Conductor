import AppKit
import SwiftUI

struct SetupView: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 22) {
            HStack(spacing: 16) {
                Image(systemName: "point.3.connected.trianglepath.dotted")
                    .font(.system(size: 40, weight: .medium))
                    .foregroundStyle(ConductorTheme.signal)
                    .frame(width: 64, height: 64)
                    .background(ConductorTheme.signal.opacity(0.12), in: RoundedRectangle(cornerRadius: 15))
                VStack(alignment: .leading, spacing: 4) {
                    Text("Set up Conductor").font(.title2.weight(.semibold))
                    Text("Install the bundled CLI and merge its handlers into your Codex hooks.")
                        .foregroundStyle(.secondary)
                }
            }

            VStack(alignment: .leading, spacing: 12) {
                setupLine("Install or update ~/.local/bin/conductor")
                setupLine("Initialize the default local project")
                setupLine("Preserve existing hooks and add Conductor handlers")
            }

            Text("After setup, restart Codex sessions and review the commands from /hooks before trusting them.")
                .font(.caption)
                .foregroundStyle(.secondary)

            HStack {
                Button("Later") {
                    model.setupNeeded = false
                    dismiss()
                }
                Spacer()
                Button("Install CLI and hooks") {
                    Task {
                        await model.installCLIAndHooks()
                        if !model.setupNeeded { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(26)
        .frame(width: 540)
        .interactiveDismissDisabled()
    }

    private func setupLine(_ text: String) -> some View {
        Label(text, systemImage: "checkmark.circle")
            .foregroundStyle(.primary)
    }
}

struct NewProjectSheet: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("New project").font(.title2.weight(.semibold))
            Text("Create an isolated Conductor namespace. This does not create terminals or worktrees.")
                .foregroundStyle(.secondary)
            TextField("project-name", text: $name)
                .textFieldStyle(.roundedBorder)
            Text(projectHint)
                .font(.caption)
                .foregroundStyle(isValidProjectID(name) ? .secondary : ConductorTheme.failure)
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Create project") {
                    Task {
                        await model.initializeProject(normalized)
                        if model.lastError == nil { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(!isValidProjectID(name))
            }
        }
        .padding(24)
        .frame(width: 460)
    }

    private var normalized: String {
        normalizedProjectID(name)
    }

    private var projectHint: String {
        guard isValidProjectID(name) else {
            return "Use 1–64 letters, numbers, or hyphens; start and end with a letter or number."
        }
        return "Sessions will use \(normalized)--brain and \(normalized)--worker-N."
    }
}

struct GoalSheet: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss
    let target: WorkerActionTarget
    @State private var objective = ""

    private var worker: WorkerSummary { target.worker }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("Send goal").font(.title2.weight(.semibold))
                    Text("\(worker.alias) · \(worker.workspace)").foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer()
                StatusDot(color: ConductorTheme.signal)
            }
            TextEditor(text: $objective)
                .font(.system(.body, design: .monospaced))
                .scrollContentBackground(.hidden)
                .padding(8)
                .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
                .overlay { RoundedRectangle(cornerRadius: 10).stroke(Color.primary.opacity(0.1)) }
                .frame(minHeight: 260)
            HStack {
                Text("The objective is delivered verbatim as a real /goal command.")
                    .font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                Button("Send goal") {
                    Task {
                        await model.sendGoal(projectID: target.projectID, worker: worker.alias, objective: objective)
                        if model.lastError == nil { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(objective.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(24)
        .frame(width: 680, height: 430)
    }
}

struct FinishSheet: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss
    let target: WorkerActionTarget
    @State private var message = ""
    @State private var status = "complete"
    @State private var confirmFinish = false

    private var worker: WorkerSummary { target.worker }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Finish \(worker.alias) manually").font(.title2.weight(.semibold))
            Text("Use this recovery action only when the normal Codex completion hook was unavailable.")
                .foregroundStyle(.secondary)
            Picker("Goal status", selection: $status) {
                Text("Complete").tag("complete")
                Text("Blocked").tag("blocked")
                Text("Manual").tag("manual")
            }
            .pickerStyle(.segmented)
            TextEditor(text: $message)
                .font(.system(.body, design: .monospaced))
                .padding(8)
                .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
                .frame(minHeight: 190)
            Text("Leave the message empty to use the most recently cached assistant response.")
                .font(.caption).foregroundStyle(.secondary)
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Finish worker…", role: .destructive) { confirmFinish = true }
            }
        }
        .padding(24)
        .frame(width: 600)
        .confirmationDialog("Finish \(worker.alias)?", isPresented: $confirmFinish) {
            Button("Finish and queue handoff", role: .destructive) {
                Task {
                    await model.finish(
                        projectID: target.projectID,
                        worker: worker.alias,
                        taskID: worker.activeTask?.id ?? "",
                        message: message,
                        status: status
                    )
                    if model.lastError == nil { dismiss() }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This ends the active local task and queues its handoff for the Brain.")
        }
    }
}

struct BrainLaunchSheet: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss
    let projectID: String
    @State private var workspace = ""
    @State private var modelID = ""
    @State private var reasoningEffort = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Open Brain terminal").font(.title2.weight(.semibold))
            Text("Conductor opens a real terminal and attaches to the project Brain session. A new session starts Codex with your selected model settings.")
                .foregroundStyle(.secondary)
            HStack {
                TextField("Workspace path", text: $workspace)
                    .textFieldStyle(.roundedBorder)
                Button("Choose…") { chooseWorkspace() }
            }
            CodexLaunchOptionsFields(modelID: $modelID, reasoningEffort: $reasoningEffort)
            launchBehaviorNote(session: project?.brainSession ?? "brain")
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Open terminal") {
                    Task { await launch() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(project == nil)
            }
        }
        .padding(24)
        .frame(width: 620)
        .onAppear { workspace = project?.state.brain.cwd ?? "" }
        .task { await model.loadModelCatalog() }
    }

    private var project: ProjectSnapshot? {
        model.snapshot?.projects.first { $0.id == projectID }
    }

    @ViewBuilder
    private func launchBehaviorNote(session: String) -> some View {
        if model.snapshot?.tmuxSessions.contains(session) == true {
            Label("The session already exists. Conductor will attach without changing its active model or effort.", systemImage: "arrow.trianglehead.merge")
                .font(.caption)
                .foregroundStyle(.secondary)
        } else {
            Label("The new session starts Codex inside tmux. Model and effort apply only to this launch.", systemImage: "sparkles")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private func launch() async {
        guard let project else { return }
        do {
            try await TerminalLauncher.launch(
                session: project.brainSession,
                workspace: workspace,
                tmuxExecutable: model.snapshot?.tmuxExecutable ?? "tmux",
                codexOptions: CodexLaunchOptions(model: modelID, reasoningEffort: reasoningEffort)
            )
            dismiss()
        } catch {
            await MainActor.run { model.lastError = error.localizedDescription }
        }
    }

    private func chooseWorkspace() {
        workspace = chooseWorkspacePath(current: workspace)
    }
}

struct NewWorkerSheet: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.dismiss) private var dismiss
    let projectID: String
    @State private var number = 1
    @State private var workspace = ""
    @State private var modelID = ""
    @State private var reasoningEffort = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Open Worker terminal").font(.title2.weight(.semibold))
            Text("Conductor opens a real terminal and attaches to the tmux session. It does not create or manage a worktree.")
                .foregroundStyle(.secondary)
            if workerCreationSupported {
                Stepper("Worker: worker-\(number)", value: $number, in: 1...99)
            } else {
                Label("This project uses a custom worker session pattern. Create the tmux session manually; Conductor will detect it automatically.", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            }
            if workerCreationSupported {
                HStack {
                    TextField("Workspace path", text: $workspace)
                        .textFieldStyle(.roundedBorder)
                    Button("Choose…") { chooseWorkspace() }
                }
                CodexLaunchOptionsFields(modelID: $modelID, reasoningEffort: $reasoningEffort)
                if model.snapshot?.tmuxSessions.contains(sessionName) == true {
                    Label("The session already exists. Conductor will attach without changing its active model or effort.", systemImage: "arrow.trianglehead.merge")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    Label("The new session starts Codex inside tmux. Model and effort apply only to this launch.", systemImage: "sparkles")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Open terminal") {
                    Task {
                        do {
                            try await TerminalLauncher.launch(
                                session: sessionName,
                                workspace: workspace,
                                tmuxExecutable: model.snapshot?.tmuxExecutable ?? "tmux",
                                codexOptions: CodexLaunchOptions(model: modelID, reasoningEffort: reasoningEffort)
                            )
                            dismiss()
                        } catch {
                            await MainActor.run { model.lastError = error.localizedDescription }
                        }
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(!workerCreationSupported)
            }
        }
        .padding(24)
        .frame(width: 560)
        .onAppear { number = suggestedWorkerNumber }
        .task { await model.loadModelCatalog() }
    }

    private var sessionName: String {
        guard let template = project?.workerSessionTemplate, !template.isEmpty else { return "unavailable" }
        return String(format: template, number)
    }

    private var project: ProjectSnapshot? {
        model.snapshot?.projects.first { $0.id == projectID }
    }

    private var workerCreationSupported: Bool { project?.workerSessionTemplate.isEmpty == false }

    private var suggestedWorkerNumber: Int {
        let aliases = project?.workers(connectedSessions: Set(model.snapshot?.tmuxSessions ?? [])).map(\.alias) ?? []
        let used = Set(aliases.map(workerNumber))
        return (1...99).first { !used.contains($0) } ?? 1
    }

    private func chooseWorkspace() {
        workspace = chooseWorkspacePath(current: workspace)
    }
}

struct CodexLaunchOptionsFields: View {
    @EnvironmentObject private var model: DashboardModel
    @Binding var modelID: String
    @Binding var reasoningEffort: String

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                TextField("Model ID — blank uses Codex default", text: $modelID)
                    .textFieldStyle(.roundedBorder)
                Menu("Choose…") {
                    Button("Use Codex default") { modelID = "" }
                    Divider()
                    ForEach(model.modelCatalog?.models ?? []) { option in
                        Button(option.displayName.isEmpty ? option.slug : "\(option.displayName) · \(option.slug)") {
                            modelID = option.slug
                            if !supportedEfforts.contains(reasoningEffort) { reasoningEffort = "" }
                        }
                    }
                }
            }
            Picker("Reasoning effort", selection: $reasoningEffort) {
                Text("Use model default").tag("")
                ForEach(supportedEfforts, id: \.self) { effort in
                    Text(effort).tag(effort)
                }
            }
            .pickerStyle(.menu)
            if let selectedModel {
                Text("\(selectedModel.displayName.isEmpty ? selectedModel.slug : selectedModel.displayName) defaults to \(selectedModel.defaultReasoningLevel).")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else if let error = model.modelCatalog?.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.secondary)
            } else {
                Text("Enter any model ID supported by your Codex installation.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .onChange(of: modelID) {
            if !reasoningEffort.isEmpty, !supportedEfforts.contains(reasoningEffort) {
                reasoningEffort = ""
            }
        }
    }

    private var selectedModel: CodexModelOption? {
        let value = modelID.trimmingCharacters(in: .whitespacesAndNewlines)
        return model.modelCatalog?.models.first { $0.slug == value }
    }

    private var supportedEfforts: [String] {
        let discovered = selectedModel?.supportedReasoningLevels.map(\.effort).filter { !$0.isEmpty } ?? []
        return discovered.isEmpty ? ["minimal", "low", "medium", "high", "xhigh", "max", "ultra"] : discovered
    }
}

private func chooseWorkspacePath(current: String) -> String {
    let panel = NSOpenPanel()
    panel.canChooseDirectories = true
    panel.canChooseFiles = false
    panel.allowsMultipleSelection = false
    return panel.runModal() == .OK ? (panel.url?.path ?? current) : current
}

struct SettingsView: View {
    @EnvironmentObject private var model: DashboardModel
    @State private var priority = TerminalLauncher.configuredPriority
    @State private var confirmUninstall = false

    var body: some View {
        TabView {
            Form {
                Section("Terminal priority") {
                    ForEach(Array(priority.enumerated()), id: \.element) { index, terminal in
                        HStack {
                            Image(systemName: terminal.iconName).frame(width: 22)
                            VStack(alignment: .leading) {
                                Text(terminal.rawValue)
                                Text(terminal.isInstalled ? "Installed" : "Not installed").font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button("Move up", systemImage: "chevron.up") { move(index, -1) }.labelStyle(.iconOnly).disabled(index == 0)
                            Button("Move down", systemImage: "chevron.down") { move(index, 1) }.labelStyle(.iconOnly).disabled(index == priority.count - 1)
                        }
                    }
                }
                Text("Conductor uses the first installed terminal in this list. macOS may request permission the first time the app opens a session.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            .padding(20)
            .tabItem { Label("Terminal", systemImage: "apple.terminal") }

            Form {
                Section("CLI and hooks") {
                    LabeledContent("Installed path", value: ConductorCLI.installedURL.path)
                    Button("Install or update CLI and hooks") { Task { await model.installCLIAndHooks() } }
                    Button("Uninstall Conductor hooks…", role: .destructive) { confirmUninstall = true }
                }
            }
            .padding(20)
            .tabItem { Label("Integration", systemImage: "link") }
        }
        .confirmationDialog("Uninstall Conductor hooks?", isPresented: $confirmUninstall) {
            Button("Uninstall hooks", role: .destructive) { Task { await model.uninstallHooks() } }
            Button("Cancel", role: .cancel) {}
        }
    }

    private func move(_ index: Int, _ offset: Int) {
        let destination = index + offset
        guard priority.indices.contains(index), priority.indices.contains(destination) else { return }
        priority.swapAt(index, destination)
        TerminalLauncher.savePriority(priority)
    }
}
