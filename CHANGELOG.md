# Changelog

All notable changes to Conductor are documented here.

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

- Independent project namespaces, each with one Sol and any number of Luna
  workers.
- Deterministic tmux naming with `<project>--sol` and
  `<project>--luna-N`.
- Automatic project inference from the current tmux session, while preserving
  the short `conductor goal luna-N` command and model-facing worker alias.
- Explicit `--project` / `-p` selection for commands outside tmux.
- `project init`, `project list`, and `project sessions` commands.
- Cross-project status through `status --all` and `status --all --json`.
- Hook fallback routing by recorded Codex session ID and workspace when tmux
  context is unavailable.
- Per-project state, locks, task files, handoff files, cache, logs, Sol
  activity, and FIFO delivery queues.

### Compatibility

- Existing `sol` and `luna-N` sessions remain the `default` project and keep
  the v0.1 state layout without migration.

## 0.1.0 — 2026-08-20

Initial release.

### Added

- Visible Sol-to-Luna delegation through named tmux sessions.
- Native `/goal` delivery without integrated Codex subagents, with the slash prefix typed separately from large objective pastes.
- Support for any number of workers named `luna-1`, `luna-2`, and so on.
- Event-driven completion through Codex `PostToolUse` and `Stop` hooks.
- Verbatim worker handoffs with a minimal worker/status/workspace envelope.
- FIFO queuing when Sol is active, with one handoff delivered after each stop.
- Race protection that requeues a handoff if Sol starts another turn before paste.
- Manual `finish`, `idle`, and `flush` recovery commands.
- Additive, backed-up hook installation and selective uninstall.
- Exact inline Sol-authored goals, plus a long-goal file fallback below Codex's persisted objective limit.
- Private local state, handoff, cache, and task files.
- Prebuilt macOS binaries for Apple Silicon and Intel.
