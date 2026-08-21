import SwiftUI

struct InspectorView: View {
    @EnvironmentObject private var model: DashboardModel

    var body: some View {
        VStack(spacing: 0) {
            Picker("Inspector", selection: $model.inspectorSection) {
                ForEach(InspectorSection.allCases) { section in
                    Text(section.rawValue).tag(section)
                }
            }
            .pickerStyle(.segmented)
            .padding(14)

            Divider()

            Group {
                switch model.inspectorSection {
                case .goal: GoalInspector()
                case .inbox: InboxInspector()
                case .log: LogInspector()
                case .health: HealthInspector()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Color(nsColor: .controlBackgroundColor).opacity(0.3))
    }
}

struct GoalInspector: View {
    @EnvironmentObject private var model: DashboardModel

    var body: some View {
        if let project = model.selectedProject, let task = model.selectedTask {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    HStack {
                        StatusDot(
                            color: ConductorTheme.taskColor(task.status),
                            pulse: task.status == "running"
                        )
                        VStack(alignment: .leading, spacing: 2) {
                            Text(task.workerAlias ?? task.workerSession).font(.headline)
                            Text(task.status.capitalized).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        if let goal = task.terminalGoalStatus, !goal.isEmpty {
                            Text(goal).font(.caption.weight(.medium)).padding(.horizontal, 7).padding(.vertical, 3).background(.quaternary, in: Capsule())
                        }
                    }

                    metadata("Workspace", task.workspace)
                    metadata("Created", formattedTimestamp(task.createdAt))
                    if task.status != "running", let finished = task.finishedAt {
                        metadata("Finished", formattedTimestamp(finished))
                    }

                    VStack(alignment: .leading, spacing: 8) {
                        Text("GOAL").font(.caption.weight(.semibold)).tracking(1.3).foregroundStyle(.secondary)
                        Text(project.goalTexts[task.id] ?? task.sentGoalObjective)
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(12)
                            .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 9))
                        if project.goalTextTruncated[task.id] == true {
                            HStack {
                                Label("Preview truncated", systemImage: "scissors")
                                    .foregroundStyle(ConductorTheme.waiting)
                                Spacer()
                                Button("Reveal full goal") {
                                    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: task.objectivePath)])
                                }
                            }
                            .font(.caption)
                        }
                    }

                    if let error = task.lastError, !error.isEmpty {
                        Label(error, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(ConductorTheme.failure)
                            .textSelection(.enabled)
                    }
                }
                .padding(16)
            }
        } else {
            ContentUnavailableView("No goal selected", systemImage: "scope", description: Text("Select a worker or recent movement."))
        }
    }

    private func metadata(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(.tertiary)
            Text(value.isEmpty ? "—" : value).font(.caption).textSelection(.enabled)
        }
    }
}

struct InboxInspector: View {
    @EnvironmentObject private var model: DashboardModel
    @State private var selectedDeliveryID: String?

    var body: some View {
        if let project = model.selectedProject, !project.orderedHandoffs.isEmpty {
            VStack(spacing: 0) {
                List(project.orderedHandoffs, selection: $selectedDeliveryID) { delivery in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(delivery.workerAlias ?? delivery.workerSession).fontWeight(.medium)
                            Spacer()
                            Text(delivery.status.capitalized)
                                .font(.caption2.weight(.semibold))
                                .foregroundStyle(delivery.status == "pending" ? ConductorTheme.waiting : .secondary)
                        }
                        Text(project.handoffMessages[delivery.id] ?? delivery.goalStatus)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }
                    .tag(delivery.id)
                }
                .frame(minHeight: 210)

                Divider()

                ScrollView {
                    if let delivery = selectedDelivery(project) {
                        VStack(alignment: .leading, spacing: 10) {
                            HStack {
                                Text(delivery.workerAlias ?? delivery.workerSession).font(.headline)
                                Spacer()
                                Text(formattedTimestamp(delivery.createdAt)).font(.caption).foregroundStyle(.secondary)
                            }
                            Text(project.handoffMessages[delivery.id] ?? "The handoff message is unavailable.")
                                .font(.system(.body, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            if project.handoffMessageTruncated[delivery.id] == true {
                                HStack {
                                    Label("Preview truncated", systemImage: "scissors")
                                        .foregroundStyle(ConductorTheme.waiting)
                                    Spacer()
                                    Button("Reveal full handoff") {
                                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: delivery.messagePath)])
                                    }
                                }
                                .font(.caption)
                            }
                        }
                        .padding(16)
                    } else {
                        Text("Select a handoff to read it.").foregroundStyle(.secondary).padding(24)
                    }
                }
            }
            .task(id: project.id) { selectedDeliveryID = project.orderedHandoffs.first?.id }
        } else {
            ContentUnavailableView("Inbox empty", systemImage: "tray", description: Text("Completed Worker handoffs appear here."))
        }
    }

    private func selectedDelivery(_ project: ProjectSnapshot) -> Delivery? {
        guard let selectedDeliveryID else { return project.orderedHandoffs.first }
        return project.state.deliveries[selectedDeliveryID]
    }
}

struct LogInspector: View {
    @EnvironmentObject private var model: DashboardModel

    var body: some View {
        if let project = model.selectedProject, !project.logTail.isEmpty {
            ScrollView([.horizontal, .vertical]) {
                Text(project.logTail)
                    .font(.system(size: 11.5, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(14)
            }
            .safeAreaInset(edge: .bottom) {
                HStack {
                    Text(project.logPath).lineLimit(1).truncationMode(.middle)
                    Spacer()
                    Button("Reveal", systemImage: "folder") {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: project.logPath)])
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(10)
                .background(.bar)
            }
        } else {
            ContentUnavailableView("No log entries", systemImage: "doc.text.magnifyingglass", description: Text("Transport activity and recovery diagnostics appear here."))
        }
    }
}

struct HealthInspector: View {
    @EnvironmentObject private var model: DashboardModel
    @State private var confirmUninstall = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("System health").font(.headline)
                        Text("Local binaries, hooks and sessions").font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Run doctor") { Task { await model.loadDoctor() } }
                }

                if let report = model.doctorReport, model.doctorProjectID == model.selectedProjectID {
                    ForEach(report.checks) { check in
                        HStack(alignment: .top, spacing: 10) {
                            Image(systemName: check.ok ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                                .foregroundStyle(check.ok ? ConductorTheme.complete : ConductorTheme.waiting)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(check.name).fontWeight(.medium)
                                Text(check.value).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                            }
                            Spacer()
                        }
                        Divider()
                    }
                    Text(report.note).font(.caption).foregroundStyle(.secondary)
                } else {
                    Button("Check setup") { Task { await model.loadDoctor() } }
                        .buttonStyle(.borderedProminent)
                }

                HStack {
                    Button("Install hooks") { Task { await model.installHooks() } }
                    Button("Uninstall hooks…", role: .destructive) { confirmUninstall = true }
                }
            }
            .padding(16)
        }
        .confirmationDialog("Uninstall Conductor hooks?", isPresented: $confirmUninstall) {
            Button("Uninstall hooks", role: .destructive) { Task { await model.uninstallHooks() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Only Conductor handlers are removed. Existing non-Conductor hooks are preserved.")
        }
        .task(id: model.selectedProjectID) { await model.loadDoctor() }
    }
}
