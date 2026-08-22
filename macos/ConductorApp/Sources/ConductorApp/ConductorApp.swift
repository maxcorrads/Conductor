import AppKit
import SwiftUI

@main
struct ConductorMacApp: App {
    @StateObject private var model = DashboardModel()

    var body: some Scene {
        Window("Conductor", id: "dashboard") {
            RootView()
                .environmentObject(model)
                .frame(minWidth: 1080, minHeight: 680)
                .task { await model.start() }
                .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
                    Task { await model.refresh() }
                }
        }
        .defaultSize(width: 1280, height: 780)
        .commands {
            CommandGroup(after: .newItem) {
                Button("Refresh") { Task { await model.refresh() } }
                    .keyboardShortcut("r", modifiers: .command)
            }
        }

        MenuBarExtra {
            MenuBarContent()
                .environmentObject(model)
        } label: {
            Label("Conductor", systemImage: menuBarIcon)
        }
        .menuBarExtraStyle(.menu)

        Settings {
            SettingsView()
                .environmentObject(model)
                .frame(width: 520, height: 360)
        }
    }

    private var menuBarIcon: String {
        let sessions = Set(model.snapshot?.tmuxSessions ?? [])
        let busy = model.snapshot?.projects.contains { project in
            project.brainBusy(sessionActivity: model.snapshot?.sessionActivity ?? [:]) || project.workers(
                connectedSessions: sessions,
                sessionActivity: model.snapshot?.sessionActivity ?? [:],
                sessionAttention: model.snapshot?.sessionAttention ?? [:]
            ).contains { $0.connected && $0.busy }
        } == true
        let pending = model.snapshot?.projects.reduce(0) { $0 + $1.pendingHandoffs.count } ?? 0
        if pending > 0 { return "tray.full.fill" }
        if busy { return "waveform.path.ecg" }
        return "point.3.connected.trianglepath.dotted"
    }
}

struct MenuBarContent: View {
    @EnvironmentObject private var model: DashboardModel
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Button("Open Conductor") {
            NSApp.activate(ignoringOtherApps: true)
            openWindow(id: "dashboard")
        }
        .keyboardShortcut("o")

        Divider()

        if let snapshot = model.snapshot {
            ForEach(snapshot.projects) { project in
                let workers = project.workers(
                    connectedSessions: Set(snapshot.tmuxSessions),
                    sessionActivity: snapshot.sessionActivity,
                    sessionAttention: snapshot.sessionAttention
                )
                Menu(project.id) {
                    Button("Open Brain…") {
                        model.brainLaunchRequest = ProjectActionTarget(projectID: project.id)
                        NSApp.activate(ignoringOtherApps: true)
                        openWindow(id: "dashboard")
                    }
                    Divider()
                    ForEach(workers) { worker in
                        Button("\(worker.alias) · \(menuStatus(worker))") {
                            openTerminal(worker.session, workspace: worker.workspace)
                        }
                        .disabled(!worker.connected)
                    }
                    if project.pendingHandoffs.count > 0 {
                        Divider()
                        Text("\(project.pendingHandoffs.count) handoff ready")
                    }
                }
            }
        } else {
            Text("Loading state…")
        }

        Divider()
        Button("Refresh") { Task { await model.refresh() } }
        Button("Quit Conductor") { NSApp.terminate(nil) }
    }

    private func openTerminal(_ session: String, workspace: String?) {
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

    private func menuStatus(_ worker: WorkerSummary) -> String {
        if !worker.connected { return "offline" }
        if worker.needsAttention { return "needs confirmation" }
        if worker.dispatchUncertain { return "dispatch unconfirmed" }
        if worker.busy { return "working" }
        if worker.waitingOnGoal { return "goal active" }
        return "idle"
    }
}
