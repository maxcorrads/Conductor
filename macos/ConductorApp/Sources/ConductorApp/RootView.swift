import AppKit
import SwiftUI

struct RootView: View {
    @EnvironmentObject private var model: DashboardModel
    @State private var showNewProject = false
    @State private var goalTarget: WorkerActionTarget?
    @State private var finishTarget: WorkerActionTarget?
    @State private var newWorkerTarget: ProjectActionTarget?
    @State private var brainLaunchTarget: ProjectActionTarget?
    @State private var forceFlushTarget: ProjectActionTarget?
    @State private var deleteProjectTarget: ProjectActionTarget?
    @State private var brainSetupTarget: ProjectActionTarget?

    var body: some View {
        NavigationSplitView {
            ProjectSidebar(
                showNewProject: $showNewProject,
                onDeleteProject: { deleteProjectTarget = ProjectActionTarget(projectID: $0.id) }
            )
                .navigationSplitViewColumnWidth(min: 190, ideal: 220, max: 280)
        } content: {
            if let project = model.selectedProject, let snapshot = model.snapshot {
                ControlRoomView(
                    project: project,
                    connectedSessions: Set(snapshot.tmuxSessions),
                    sessionActivity: snapshot.sessionActivity,
                    onGoal: { goalTarget = WorkerActionTarget(projectID: project.id, worker: $0) },
                    onFinish: { finishTarget = WorkerActionTarget(projectID: project.id, worker: $0) },
                    onOpenBrain: { brainLaunchTarget = ProjectActionTarget(projectID: project.id) },
                    onFocusBrain: { focusTerminal(session: project.brainSession) },
                    onBrainSetup: { brainSetupTarget = ProjectActionTarget(projectID: project.id) },
                    onNewWorker: { newWorkerTarget = ProjectActionTarget(projectID: project.id) },
                    onForceFlush: { forceFlushTarget = ProjectActionTarget(projectID: project.id) }
                )
            } else {
                ContentUnavailableView("No project selected", systemImage: "point.3.connected.trianglepath.dotted", description: Text("Initialize a project to begin."))
            }
        } detail: {
            InspectorView()
                .navigationSplitViewColumnWidth(min: 300, ideal: 360, max: 500)
        }
        .tint(ConductorTheme.signal)
        .toolbar {
            ToolbarItemGroup {
                if model.isRefreshing { ProgressView().controlSize(.small) }
                Button("Refresh", systemImage: "arrow.clockwise") { Task { await model.refresh() } }
            }
        }
        .sheet(isPresented: $showNewProject) { NewProjectSheet() }
        .sheet(item: $goalTarget) { GoalSheet(target: $0) }
        .sheet(item: $finishTarget) { FinishSheet(target: $0) }
        .sheet(item: $brainLaunchTarget) { BrainLaunchSheet(projectID: $0.projectID) }
        .sheet(item: $newWorkerTarget) { NewWorkerSheet(projectID: $0.projectID) }
        .sheet(item: $brainSetupTarget) { BrainSetupSheet(projectID: $0.projectID) }
        .sheet(isPresented: $model.setupNeeded) { SetupView() }
        .confirmationDialog("Force handoff delivery?", isPresented: forceFlushBinding) {
            Button("Force delivery", role: .destructive) {
                guard let id = forceFlushTarget?.projectID else { return }
                forceFlushTarget = nil
                Task { await model.flush(projectID: id, force: true) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Use this only after visually confirming that the Brain is at an empty composer.")
        }
        .confirmationDialog("Delete project?", isPresented: deleteProjectBinding, titleVisibility: .visible) {
            Button("Delete \(deleteProjectTarget?.projectID ?? "project")", role: .destructive) {
                guard let id = deleteProjectTarget?.projectID else { return }
                deleteProjectTarget = nil
                Task { await model.deleteProject(id) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This permanently removes the project’s local Conductor state, goals, handoffs, and logs. It never deletes workspaces or terminal sessions. Close the project’s Brain and Workers before continuing.")
        }
        .alert("Conductor could not complete the action", isPresented: errorBinding) {
            Button("OK") { model.lastError = nil }
        } message: {
            Text(model.lastError ?? "Unknown error")
        }
        .alert("Conductor", isPresented: noticeBinding) {
            Button("OK") { model.notice = nil }
        } message: {
            Text(model.notice ?? "Done")
        }
        .onReceive(model.$brainLaunchRequest) { request in
            guard let request else { return }
            brainLaunchTarget = request
            model.brainLaunchRequest = nil
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(get: { model.lastError != nil }, set: { if !$0 { model.lastError = nil } })
    }

    private var noticeBinding: Binding<Bool> {
        Binding(get: { model.notice != nil }, set: { if !$0 { model.notice = nil } })
    }

    private var forceFlushBinding: Binding<Bool> {
        Binding(
            get: { forceFlushTarget != nil },
            set: { if !$0 { forceFlushTarget = nil } }
        )
    }

    private var deleteProjectBinding: Binding<Bool> {
        Binding(
            get: { deleteProjectTarget != nil },
            set: { if !$0 { deleteProjectTarget = nil } }
        )
    }

    private func focusTerminal(session: String) {
        Task {
            do {
                try await TerminalLauncher.focus(
                    session: session,
                    tmuxExecutable: model.snapshot?.tmuxExecutable ?? "tmux"
                )
            } catch {
                await MainActor.run { model.lastError = error.localizedDescription }
            }
        }
    }
}

struct ProjectSidebar: View {
    @EnvironmentObject private var model: DashboardModel
    @Binding var showNewProject: Bool
    let onDeleteProject: (ProjectSnapshot) -> Void

    var body: some View {
        List(selection: $model.selectedProjectID) {
            Section("Projects") {
                ForEach(model.snapshot?.projects ?? []) { project in
                    HStack(spacing: 10) {
                        StatusDot(
                            color: brainColor(project),
                            pulse: brainConnected(project) && project.brainBusy(
                                sessionActivity: model.snapshot?.sessionActivity ?? [:]
                            )
                        )
                        VStack(alignment: .leading, spacing: 2) {
                            Text(project.id).fontWeight(.medium)
                            Text(projectSummary(project))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        if !project.pendingHandoffs.isEmpty {
                            Text("\(project.pendingHandoffs.count)")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.white)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(ConductorTheme.waiting, in: Capsule())
                        }
                        if project.id != "default" {
                            Menu {
                                Button("Delete project…", role: .destructive) {
                                    onDeleteProject(project)
                                }
                            } label: {
                                Image(systemName: "ellipsis.circle")
                                    .foregroundStyle(.secondary)
                            }
                            .menuStyle(.borderlessButton)
                            .fixedSize()
                        }
                    }
                    .tag(project.id)
                }
            }
        }
        .navigationTitle("Conductor")
        .safeAreaInset(edge: .bottom) {
            Button {
                showNewProject = true
            } label: {
                Label("New project", systemImage: "plus")
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.plain)
            .padding(12)
        }
    }

    private func projectSummary(_ project: ProjectSnapshot) -> String {
        if !brainConnected(project) { return "Brain offline" }
        if project.brainBusy(sessionActivity: model.snapshot?.sessionActivity ?? [:]) { return "Brain active" }
        let sessions = Set(model.snapshot?.tmuxSessions ?? [])
        let busy = project.workers(
            connectedSessions: sessions,
            sessionActivity: model.snapshot?.sessionActivity ?? [:]
        ).filter { $0.connected && $0.busy }.count
        if busy > 0 { return "\(busy) worker active" }
        if !project.pendingHandoffs.isEmpty { return "Handoff ready" }
        return "Idle"
    }

    private func brainConnected(_ project: ProjectSnapshot) -> Bool {
        model.snapshot?.tmuxSessions.contains(project.brainSession) == true
    }

    private func brainColor(_ project: ProjectSnapshot) -> Color {
        ConductorTheme.statusColor(
            connected: brainConnected(project),
            busy: project.brainBusy(sessionActivity: model.snapshot?.sessionActivity ?? [:])
        )
    }
}

struct ControlRoomView: View {
    @EnvironmentObject private var model: DashboardModel
    let project: ProjectSnapshot
    let connectedSessions: Set<String>
    let sessionActivity: [String: Bool]
    let onGoal: (WorkerSummary) -> Void
    let onFinish: (WorkerSummary) -> Void
    let onOpenBrain: () -> Void
    let onFocusBrain: () -> Void
    let onBrainSetup: () -> Void
    let onNewWorker: () -> Void
    let onForceFlush: () -> Void

    private var workers: [WorkerSummary] {
        project.workers(connectedSessions: connectedSessions, sessionActivity: sessionActivity)
    }
    private var brainConnected: Bool { connectedSessions.contains(project.brainSession) }
    private var brainBusy: Bool { project.brainBusy(sessionActivity: sessionActivity) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                header
                brainCard
                score
                recentTasks
            }
            .padding(24)
        }
        .navigationTitle(project.id)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 4) {
                Text("CONTROL ROOM").font(.caption.weight(.semibold)).tracking(1.6).foregroundStyle(ConductorTheme.signal)
                Text(project.id == "default" ? "Default project" : project.id)
                    .font(.system(size: 28, weight: .semibold, design: .rounded))
                Text("\(workers.filter(\.connected).count) connected · \(workers.filter { $0.connected && $0.busy }.count) working")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button(action: onOpenBrain) {
                Label("Open Brain…", systemImage: "macwindow.and.cursorarrow")
            }
            .buttonStyle(.borderedProminent)
        }
    }

    private var brainCard: some View {
        Panel {
            HStack(spacing: 14) {
                ZStack {
                    RoundedRectangle(cornerRadius: 11).fill(ConductorTheme.signal.opacity(0.12)).frame(width: 44, height: 44)
                    Image(systemName: "wand.and.stars").foregroundStyle(ConductorTheme.signal).font(.title3)
                }
                VStack(alignment: .leading, spacing: 3) {
                    Text("Brain").font(.headline)
                    Text(project.brainSession).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                }
                Spacer()
                StatusDot(
                    color: ConductorTheme.statusColor(connected: brainConnected, busy: brainBusy),
                    pulse: brainConnected && brainBusy
                )
                Text(brainConnected ? (brainBusy ? "Active" : "Idle") : "Offline")
                    .foregroundStyle(.secondary)
                Button("Focus terminal", systemImage: "viewfinder", action: onFocusBrain)
                    .labelStyle(.iconOnly)
                    .help("Focus an existing terminal for this Brain without opening a new one")
                    .disabled(!brainConnected)
                Menu {
                    Button("Brain setup prompt…", systemImage: "doc.on.clipboard", action: onBrainSetup)
                    Divider()
                    Button("Mark idle") { Task { await model.markIdle(projectID: project.id) } }
                        .disabled(brainConnected && brainBusy)
                    Button("Deliver next handoff") { Task { await model.flush(projectID: project.id, force: false) } }
                    Button("Force delivery…", role: .destructive, action: onForceFlush)
                } label: { Image(systemName: "ellipsis.circle") }
                .menuStyle(.borderlessButton)
                .fixedSize()
            }
        }
    }

    private var score: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("WORKER SCORE").font(.caption.weight(.semibold)).tracking(1.4).foregroundStyle(.secondary)
                Spacer()
                Button(action: onNewWorker) { Label("Open Worker…", systemImage: "plus") }
                    .buttonStyle(.borderless)
            }
            if workers.isEmpty {
                ContentUnavailableView("No Worker sessions", systemImage: "waveform.path", description: Text("Open a real terminal session and start Codex to connect a worker."))
                    .frame(maxWidth: .infinity, minHeight: 180)
                    .background(.quaternary.opacity(0.25), in: RoundedRectangle(cornerRadius: 14))
            } else {
                VStack(spacing: 10) {
                    ForEach(workers) { worker in
                        WorkerRow(worker: worker, selected: model.selectedWorkerSession == worker.session) {
                            model.selectedWorkerSession = worker.session
                            model.selectedTaskID = worker.activeTask?.id
                        } onOpen: {
                            openTerminal(session: worker.session, workspace: worker.workspace)
                        } onFocus: {
                            focusTerminal(session: worker.session)
                        } onGoal: {
                            onGoal(worker)
                        } onFinish: {
                            onFinish(worker)
                        }
                    }
                }
                .overlay(alignment: .leading) {
                    Rectangle()
                        .fill(ConductorTheme.signal.opacity(0.22))
                        .frame(width: 2)
                        .padding(.leading, 30)
                        .padding(.vertical, 26)
                        .allowsHitTesting(false)
                }
            }
        }
    }

    private var recentTasks: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("RECENT MOVEMENTS").font(.caption.weight(.semibold)).tracking(1.4).foregroundStyle(.secondary)
            ForEach(Array(project.orderedTasks.prefix(6))) { task in
                Button {
                    model.selectedTaskID = task.id
                    model.selectedWorkerSession = task.workerSession
                    model.inspectorSection = .goal
                } label: {
                    HStack(spacing: 10) {
                        Image(systemName: ConductorTheme.taskSymbol(task.status))
                            .foregroundStyle(ConductorTheme.taskColor(task.status))
                            .frame(width: 18)
                        Text(task.workerAlias ?? task.workerSession).fontWeight(.medium)
                        Text(project.goalTexts[task.id] ?? task.sentGoalObjective)
                            .lineLimit(1)
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text(formattedTimestamp(task.createdAt)).font(.caption).foregroundStyle(.tertiary)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                Divider()
            }
            if project.historyTruncated {
                Text("Showing the newest \(project.state.tasks.count) of \(project.taskCount) goals and \(project.state.deliveries.count) of \(project.handoffCount) handoffs.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func openTerminal(session: String, workspace: String?) {
        Task {
            do {
                try await TerminalLauncher.attach(
                    session: session,
                    workspace: workspace,
                    tmuxExecutable: model.snapshot?.tmuxExecutable ?? "tmux"
                )
            }
            catch { await MainActor.run { model.lastError = error.localizedDescription } }
        }
    }

    private func focusTerminal(session: String) {
        Task {
            do {
                try await TerminalLauncher.focus(
                    session: session,
                    tmuxExecutable: model.snapshot?.tmuxExecutable ?? "tmux"
                )
            } catch {
                await MainActor.run { model.lastError = error.localizedDescription }
            }
        }
    }
}

struct WorkerRow: View {
    let worker: WorkerSummary
    let selected: Bool
    let onSelect: () -> Void
    let onOpen: () -> Void
    let onFocus: () -> Void
    let onGoal: () -> Void
    let onFinish: () -> Void

    var body: some View {
        HStack(spacing: 14) {
            ZStack {
                Circle().fill(Color(nsColor: .controlBackgroundColor)).frame(width: 38, height: 38)
                StatusDot(color: ConductorTheme.statusColor(connected: worker.connected, busy: worker.busy), pulse: worker.connected && worker.busy)
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 7) {
                    Text(worker.alias).font(.headline)
                    Text(worker.connected ? (worker.busy ? "Working" : "Ready") : "Offline")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(ConductorTheme.statusColor(connected: worker.connected, busy: worker.busy))
                }
                Text(worker.activeTask?.sentGoalObjective ?? (worker.workspace.isEmpty ? worker.session : worker.workspace))
                    .font(worker.activeTask == nil ? .caption : .subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            Button("Focus terminal", systemImage: "viewfinder", action: onFocus)
                .labelStyle(.iconOnly)
                .help("Focus an existing terminal without opening a new one")
                .disabled(!worker.connected)
            Button("Terminal", systemImage: "apple.terminal", action: onOpen)
                .labelStyle(.iconOnly)
                .help(worker.connected ? "Open another real terminal attached to this session" : "The tmux session is offline")
                .disabled(!worker.connected)
            Button("Goal", systemImage: "paperplane", action: onGoal)
                .labelStyle(.iconOnly)
                .help("Send goal")
                .disabled(!worker.connected || worker.busy || worker.activeTask != nil)
            if worker.activeTask != nil {
                Button("Finish", systemImage: "checkmark.circle", action: onFinish).labelStyle(.iconOnly).help("Finish manually")
            }
        }
        .padding(14)
        .background(selected ? ConductorTheme.signal.opacity(0.1) : Color(nsColor: .controlBackgroundColor).opacity(0.52), in: RoundedRectangle(cornerRadius: 12))
        .overlay {
            RoundedRectangle(cornerRadius: 12).stroke(selected ? ConductorTheme.signal.opacity(0.4) : Color.primary.opacity(0.05))
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: onSelect)
    }
}
