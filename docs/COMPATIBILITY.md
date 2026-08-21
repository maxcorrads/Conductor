# Compatibility

## Target environment

Conductor targets:

- macOS Apple Silicon (`darwin/arm64`);
- macOS Intel (`darwin/amd64`);
- official Codex CLI builds that expose:
  - `/goal`;
  - the `update_goal` local function tool;
  - command hooks;
  - `PostToolUse` fields `tool_name`, `tool_input`, and `tool_response`;
  - `Stop.last_assistant_message`;
- tmux with `load-buffer`, `paste-buffer`, `send-keys`, and `capture-pane`.

It supports the unprefixed `default` project plus any number of named project
namespaces using `<project>--brain` and `<project>--worker-N`.

The implementation and documentation were checked against the public Codex hook and goal implementation available on 2026-08-21.

## Feature detection

`conductor doctor` runs `codex features list` and expects `goals` and `hooks` to be enabled. Its fallback reads `~/.codex/config.toml` only when the command is unavailable.

Current Codex releases enable hooks by default. If `/goal` is absent, enable goals with:

```bash
codex features enable goals
```

## Hook configuration

Conductor writes standard user-level `~/.codex/hooks.json` groups for:

- `SessionStart`;
- `UserPromptSubmit`;
- `PostToolUse` matching `^update_goal$`;
- `Stop`.

Existing groups remain present. Codex command-hook trust is separate from feature enablement.

## Completion recovery

The primary path remains `PostToolUse(update_goal)` followed by `Stop`. If a worker `Stop` contains a final message but no goal has ever been observed, the Stop hook waits once and rechecks local Codex state exactly once. A later worker turn or any real goal status cancels that check. Only a reliably goal-less turn may produce an `implicit` handoff; unavailable or unreadable local sources leave the task open for manual recovery. There is no recurring loop and no model polling.

Codex documents transcript JSON as unstable. Conductor's transcript reader therefore:

- reads only a bounded tail;
- scans for known goal-update shapes;
- requires conservative thread/objective matches;
- never serves as the preferred path;
- can be bypassed with manual `conductor finish`.

The optional SQLite compatibility fallback tries known current and historical local goal database locations only when the `sqlite3` command exists. It is not a required dependency.

## tmux versions

Conductor first uses bracketed paste (`paste-buffer -p`). If tmux rejects that option, it retries without `-p` for older versions. Before a new worker assignment it types `/goal ` as literal keys, then pastes the objective. A bounded `capture-pane` probe recognizes only the exact Codex **Replace goal?** markers and confirms the selected replacement when necessary. This avoids both stale-goal modals and the TUI edge where a large whole-command paste can bypass slash-command dispatch.

## Build compatibility

The source requires Go 1.22 or newer. It uses only the Go standard library and Unix `flock`/process-session facilities. The current build tags support Darwin and Linux for development/testing; the release package is macOS-focused.

## Not guaranteed

Conductor does not currently support:

- Windows;
- Terminal.app/iTerm automation without tmux;
- Codex IDE/desktop surfaces that do not run inside the named tmux pane;
- Claude, Cursor, Pi, or arbitrary agent CLIs;
- integrated Codex subagents;
- several Codex panes inside one Conductor session name;
- automatic session resume or context management.
- multiple Brain sessions inside one project namespace (use separate project
  IDs instead).
