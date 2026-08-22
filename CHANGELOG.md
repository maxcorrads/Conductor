# Changelog

All notable changes to Conductor are documented here.

Historical entries use the current **Brain** and **Worker** role terminology.
Session spellings from releases before 0.3 are described as legacy labels because
0.3 intentionally replaces that namespace instead of migrating it.

## 0.4.1 — 2026-08-22

### Fixed

- Goal dispatch now waits for an observable Codex acknowledgement, confirms a
  delayed **Replace goal?** dialog, refuses to type into an active turn, and
  preserves ambiguous post-submit outcomes as a backward-compatible running
  task marked `dispatch_state: uncertain` until a later hook or explicit user
  action resolves them.
- Existing 0.4.0 configuration files automatically receive the new 10-second
  `goal_dispatch_timeout_ms` default without requiring a manual migration.
- The macOS dashboard now shows replacement dialogs as **Needs confirmation**
  and idle Workers with a running Conductor task as **Goal active**, instead of
  incorrectly presenting either state as green **Ready**.

## 0.4.0 — 2026-08-21

### Added

- Generated Brain setup prompt with project identity, recorded workspace,
  connected Worker sessions, and the Conductor delegation contract. The macOS
  app can copy it or send it after confirmation.
- Safe `brain setup` delivery that refuses to paste while Brain is working or
  while its Codex composer contains an unsent draft.
- Named-project deletion from the macOS app and CLI, with explicit confirmation,
  connected-session refusal, path containment checks, and no workspace or tmux
  deletion. The default project remains protected.
- Separate **Focus terminal** actions for Brain and Worker sessions. They raise
  an already-open Terminal or iTerm2 window without opening a fallback window.

### Changed

- Worker launch sheets now start with the Brain's last recorded workspace while
  remaining editable for independent worktrees.
- New Terminal/iTerm2 windows receive stable Conductor session titles so later
  focus actions can identify them reliably.

### Fixed

- Brain and Worker activity now follows the latest visible Codex lifecycle
  marker, so an older `Working` line cannot keep an idle or paused session active.
- Native app status surfaces prefer the current live session probe over stale
  hook activity flags.

## 0.3.0 — 2026-08-21

### Added

- Native macOS SwiftUI app with a window and menu bar status surface.
- Project, Brain, and Worker dashboard with goals, handoffs, logs, health checks,
  manual recovery commands, and first-run CLI/hook installation.
- Configurable automatic Terminal/iTerm2 selection that opens visible tmux
  sessions in a real terminal without creating or managing worktrees.
- Brain and Worker launch sheets with a free-form model ID, installed-catalog
  suggestions, and model-aware reasoning-effort selection.
- Stable JSON snapshots for GUI state, bounded goal/handoff content, log tails,
  inbox data, and doctor checks.
- Universal Apple Silicon/Intel app packaging with Developer ID signing,
  notarization, stapling, and protected GitHub Release publication.

### Changed

- Renamed coordinator and worker roles end to end to **Brain** and **Worker**,
  including tmux sessions (`--brain`, `--worker-N`), hook routing, prompts, CLI,
  JSON, configuration, and the native app.
- Advanced configuration, state, and GUI snapshot schemas to version 2. Version
  1 runtime data is intentionally rejected so 0.3 starts from a clean namespace.

## 0.2.1 — 2026-08-21

### Added

- Verified installable macOS archives in CI and automatic GitHub Release
  publication for version tags.

### Fixed

- Clear stale Codex goals before delegation and handle the bounded
  **Replace goal?** confirmation path.
- Recover ambiguous goal-less worker completions with one delayed local
  reconciliation, without recurring polling or additional model turns.
- Explain when an installation is running from a source checkout without Go
  or bundled release binaries.

## 0.2.0 — 2026-08-20

### Added

- Independent project namespaces, each with one Brain and any number of Worker
  workers.
- Deterministic tmux naming using the release's legacy role labels.
- Automatic project inference from the current tmux session, while preserving
  the short worker command and model-facing worker alias.
- Explicit `--project` / `-p` selection for commands outside tmux.
- `project init`, `project list`, and `project sessions` commands.
- Cross-project status through `status --all` and `status --all --json`.
- Hook fallback routing by recorded Codex session ID and workspace when tmux
  context is unavailable.
- Per-project state, locks, task files, handoff files, cache, logs, Brain
  activity, and FIFO delivery queues.

### Compatibility

- Existing pre-0.3 sessions remain the `default` project and keep the v0.1 state
  layout without migration within that release line.

## 0.1.0 — 2026-08-20

Initial release.

### Added

- Visible Brain-to-Worker delegation through named tmux sessions.
- Native `/goal` delivery without integrated Codex subagents, with the slash prefix typed separately from large objective pastes.
- Support for any number of numbered Worker sessions.
- Event-driven completion through Codex `PostToolUse` and `Stop` hooks.
- Verbatim worker handoffs with a minimal worker/status/workspace envelope.
- FIFO queuing when Brain is active, with one handoff delivered after each stop.
- Race protection that requeues a handoff if Brain starts another turn before paste.
- Manual `finish`, `idle`, and `flush` recovery commands.
- Additive, backed-up hook installation and selective uninstall.
- Exact inline Brain-authored goals, plus a long-goal file fallback below Codex's persisted objective limit.
- Private local state, handoff, cache, and task files.
- Prebuilt macOS binaries for Apple Silicon and Intel.
