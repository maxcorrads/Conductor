# Architecture

## Scope

Conductor is a deterministic transport between independent, visible Codex CLI
sessions. Each project namespace owns one Sol coordinator and any number of
Luna workers. The human owns every terminal and all context-management
decisions.

Conductor is deliberately not an agent framework.

## Components

```text
┌────────────────────────────────────────────────────────────────────┐
│ tmux session: project1--sol                                        │
│ Codex CLI / Sol                                                    │
│                                                                    │
│ shell: conductor goal luna-1 --stdin                              │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ tmux load-buffer + paste-buffer
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ tmux session: project1--luna-1                                     │
│ Codex CLI / Luna, separate worktree                                │
│                                                                    │
│ /goal <Sol-authored objective>                                     │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ Codex hooks
                               │ PostToolUse(update_goal)
                               │ Stop(last_assistant_message)
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ conductor executable                                               │
│                                                                    │
│ state lock → private handoff file → FIFO delivery reservation      │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ tmux paste when Sol is idle
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ Sol receives minimal envelope + verbatim Luna response             │
└────────────────────────────────────────────────────────────────────┘
```

## No daemon and no polling

Every action starts from an event:

- Sol explicitly invokes `conductor goal`.
- Codex invokes a lifecycle hook.
- The human explicitly invokes a recovery command.

After a Sol `Stop`, Conductor may start a detached one-shot `_deliver` helper. The helper sleeps for the configured short delay, verifies that Sol is still idle, sends one message, and exits.

An ambiguous Luna `Stop` waits once inside the hook process, then checks local transcript/database and Conductor state exactly once. Any observed goal invalidates its token. There is no reconciliation daemon or loop.

## Project routing

Session names are the routing table. The default project uses `sol` and
`luna-N`; a named project uses `<project>--sol` and
`<project>--luna-N`.

When `conductor goal luna-1` runs inside `project1--sol`, the current tmux
session resolves project `project1`, and the physical worker target becomes
`project1--luna-1`. The logical alias remains `luna-1` in command output and
handoffs, avoiding model-facing namespace overhead.

Codex hooks use the same current-session mapping. If tmux context is missing,
the hook router searches project states by exact Codex session ID and then by
the most specific recorded workspace path.

## State model

Conductor persists one small JSON state per project, each guarded by its own
advisory file lock. The default project retains `~/.conductor/state.json`;
named projects use `~/.conductor/projects/<project>/state.json`.

### Sol activity

- `Busy`: whether Conductor believes Sol is in a turn or has a delivery reserved/injected.
- `TurnID`: the active Codex turn id when known.
- `ReservedDelivery`: one pending handoff currently reserved for delivery.

### Worker task

One `running` task is allowed per Luna session. A task stores:

- worker pane and workspace captured at delegation;
- full original goal file;
- compact objective actually passed to `/goal`;
- Codex session/transcript identifiers when available;
- pending, observed, and final goal status;
- a short-lived reconciliation token when a goal-less Stop needs one local recheck;
- path to the final handoff.

### Delivery

A delivery transitions:

```text
pending → sending → delivered
            │
            └──── failure/race → pending
```

A stale `sending` reservation older than two minutes is recovered to `pending` on the next locked state update.

All delivery reservations and FIFO queues are project-local. A worker in one
project cannot reserve, delay, or wake another project's Sol.

## Completion path

### Primary path

1. `PostToolUse` matches the local function tool `update_goal`.
2. Conductor reads the requested status from `tool_input`.
3. When `tool_response` is present, Conductor requires its returned goal object to confirm the same status. This avoids treating a failed tool call as completion.
4. The status is stored as pending for that worker task.
5. The following `Stop` provides `last_assistant_message`.
6. Conductor writes those exact bytes to a private handoff file and finishes the task.

### Compatibility and implicit path

If the terminal `PostToolUse` signal was unavailable, Conductor first tries:

1. the tail of the session transcript;
2. the local Codex goal database through the `sqlite3` command, when available.

Both fallbacks require conservative session/objective matching. They are secondary because Codex explicitly describes transcript JSON as unstable.

If a worker `Stop` has a final assistant message, no real goal has ever been observed, and neither local source contains a matching goal, Conductor stores a unique reconciliation token and performs one delayed in-hook recheck. It forwards the cached message as status `implicit` only when the worker is still idle, the token is unchanged, and no goal appeared. Any new worker prompt, any `update_goal`, an unavailable goal source, or unreadable local state cancels the inference.

### Manual path

`conductor finish` ends an active local worker task using explicit stdin/file content or the latest cached worker response.

## Sol delivery race handling

A handoff must never become an accidental steer inside an active Sol turn.

When Sol stops:

1. Sol is marked idle.
2. The oldest pending handoff is atomically reserved.
3. A one-shot delivery helper starts with a short delay.
4. Before paste, it rereads state.
5. If a new Sol `TurnID` exists, it requeues the handoff.
6. Otherwise it resolves the current Sol pane, pastes, presses Enter, and marks the delivery complete.

Worker completion uses the same reservation state. If Sol is already idle, delivery happens synchronously after the same short safety delay; no persistent helper or polling loop is started.

## tmux transport

Prompts are never passed as shell arguments to `tmux send-keys`.

For normal handoffs, Conductor:

1. creates a random tmux buffer name;
2. streams the exact prompt through stdin to `tmux load-buffer`;
3. uses `paste-buffer` in bracketed-paste mode when supported;
4. sends one Enter key.

For delegation, Conductor clears the composer, types `/goal clear`, waits briefly for Codex to persist the clear, then types the literal `/goal ` prefix with `send-keys -l`. After a second short pause it pastes only the objective and sends Enter. Keeping the slash-command prefix outside a large paste ensures Codex recognizes the command while preserving multiline objective text.

A bounded local `capture-pane` probe looks for all exact **Replace goal?** markers. If clearing and setting race, it confirms the already-selected **Replace current goal** action. The probe has a deadline, sends no prompt to a model, and stops immediately after confirmation.

This avoids shell interpretation and does not summarize or rewrite the objective.

## Token-efficiency choices

- Conductor itself runs no model.
- There is no polling prompt from Sol and no recurring worker-status loop.
- Goal command output is only `delegated luna-N`.
- Project prefixes are used only for local tmux routing and never appended to
  Sol-authored goals or Luna handoffs.
- Inline worker objectives are exactly Sol-authored; Conductor adds no relay sentence or mandatory handoff schema.
- The handoff envelope contains only worker, status, and workspace.
- Handoff content is not copied into an intermediate summary.
- Sol can instruct Luna to store large details in the worktree and return only a selective pointer.

## Failure philosophy

Hooks are non-blocking: the hidden `conductor hook` command always emits valid empty JSON and logs internal failures instead of trapping Codex in a retry/continuation loop.

Transport state is recoverable with inspectable files and explicit CLI commands. Conductor favors a queued message over unsafe injection.
