# Native macOS app

Conductor.app is a SwiftUI control surface for the existing local Go transport.
The CLI remains authoritative for state locking, goal dispatch, handoff
delivery, hooks, and recovery commands.

## Requirements

- macOS 14 or newer;
- `tmux` and Codex CLI available on the local machine;
- existing visible Brain/Worker tmux sessions and user-managed worktrees.

The app presents these technical roles as **Brain** and **Worker**. Their tmux
session names use `--brain` and `--worker-N` for deterministic CLI and hook
routing; the labels do not constrain which Codex model runs in a session.

The app is universal (`arm64` and `x86_64`) and includes a universal Conductor
CLI under `Contents/Resources/conductor`.

## First launch

The setup sheet copies the bundled CLI to `~/.local/bin/conductor`, initializes
the default project, and additively installs Conductor's Codex hooks. Existing
non-Conductor hooks are preserved. Review and trust the resulting commands from
`/hooks` inside each Codex session.

## Terminal integration

The app detects Terminal.app and iTerm2. Their priority is configurable in
Settings. Opening Brain or Worker asks the selected terminal to run:

```text
<configured-tmux-path> new-session -A -s <session>
```

The tmux executable is the absolute path resolved by the CLI, so Terminal and
iTerm2 use the same configured tmux server even with a minimal app environment.
When an existing workspace is known, the terminal changes to it first; a stale
or removed worktree path does not prevent attaching to the session. macOS
asks for Apple Events permission the first time Conductor controls a terminal.
For a new Brain or Worker session, the launch sheet can use the Codex default or
an editable model ID and a supported reasoning effort. The app reads the
installed CLI's current model catalog, falling back to its bundled catalog when
the live catalog is unavailable, and starts Codex with
`--model` plus a per-launch `model_reasoning_effort` override. Attaching to an
existing tmux session never changes its active model or effort. The app does not
create a worktree, type model prompts, or hide the terminal session.

## Data and refresh model

The app invokes `conductor gui snapshot` and decodes its versioned JSON schema.
The bundled CLI is authoritative for both snapshots and interactive app
commands, so choosing **Later** never mixes an older installed CLI with the app
schema. The installed CLI is reserved for persistent Codex hooks; setup updates
older versions, preserves newer versions, and never performs a downgrade.
Health checks run through that compatible installed CLI so hook ownership is
verified against the executable path actually stored in Codex configuration.
It watches Conductor's private project directories for changes rather than
polling a model or starting a daemon. A lightweight `gui sessions` probe every
three seconds detects tmux sessions created or closed and whether their visible
Codex pane is currently generating, without reading goal, handoff, or log
files. A full coalesced snapshot runs only when that session or activity state
changes. Live pane activity is independent from the persistent goal lifecycle:
for example, `Goal stalled` can coexist with an active turn. This remains active
while only the menu bar is visible. Full snapshots include the newest 100 history
records plus active tasks and pending handoffs, capped at 200 records of each
kind. Text is capped at 16 MiB across all projects, with per-record ceilings of
1 MiB for goals, 2 MiB for handoffs, and 256 KiB for each project log tail. The
dashboard marks truncated history and text previews explicitly and can reveal
the complete local source file.

All Conductor state remains local. The app has no network client and no direct
OpenAI API integration. Opening a Brain or Worker launch sheet asks the user's
authenticated Codex CLI for its current model catalog; that CLI command can
refresh the catalog over the network. If it is unavailable, the app falls back
to the CLI's bundled catalog and still permits a free-form model ID.

## Confirmed actions

The app requires explicit confirmation before:

- `flush --force`;
- manual `finish`;
- deleting a named project;
- uninstalling Conductor hooks.

Normal goal dispatch, `idle`, project initialization, terminal opening, and
additive hook installation do not add an extra confirmation.

The Worker launch sheet starts with the Brain's last recorded workspace. The
user can still choose a different directory before opening the real terminal.

The Brain menu exposes a generated setup prompt containing the project,
Brain/workspace identity, connected Workers, their current Codex model and
reasoning effort when reported, and the Conductor delegation rules. Current
session metadata is read from Codex's local thread state using the session id
already learned from hooks; unavailable values are rendered as **not reported
by Codex**. Conductor refreshes this metadata when the sheet opens and again
immediately before copy or send, so a model or effort change in an otherwise
idle session does not leave the prompt on the periodic session probe's cached
snapshot. A hook observation must also be newer than the current tmux session;
legacy state without that observation remains unavailable until the next hook,
and a session whose active pane no longer runs Codex has no current profile.
The prompt can be copied at any time. Direct send requires
confirmation, a connected idle Brain, and a final CLI check that the Codex
composer is empty. Copy and send both stop without using the cached prompt if
their just-in-time snapshot refresh fails.

Brain and Worker cards also provide **Focus terminal**, which only raises an
already-open matching Terminal or iTerm2 window. It never opens a fallback
window; the existing attach action remains separate.

## Developer build

```bash
swift test --package-path macos/ConductorApp
make app
```

`make app` builds universal Swift and Go executables, assembles
`release/Conductor.app`, applies an ad-hoc signature when no distribution
identity is provided, and writes `release/Conductor-vX.Y.Z-macos.zip` plus its
SHA-256 checksum.

`BUILD_NUMBER` may be set to a positive integer for `CFBundleVersion`; local
builds default to `1`, while GitHub Actions uses its monotonically increasing
run number. `VERSION` remains the user-facing semantic version.

For Developer ID distribution, set `SIGNING_IDENTITY` to the SHA-1 identity in
an isolated build keychain. The tag release workflow uses the protected
`macos-release` GitHub Environment and then submits, staples, and verifies the
app before publication.
