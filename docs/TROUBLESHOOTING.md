# Troubleshooting

## First diagnostics

```bash
conductor doctor
conductor status
conductor inbox
```

Logs are in:

```text
~/.conductor/logs/conductor.log                       # default project
~/.conductor/projects/<project>/logs/conductor.log   # named project
```

Use `CONDUCTOR_HOME` instead of `~/.conductor` when you configured a custom home.

For commands run outside the affected tmux session, select the project:

```bash
conductor -p project1 status
conductor -p project1 inbox
```

## Hooks do not fire

Symptoms:

- tasks remain `running` after Worker finishes;
- Brain never receives the handoff;
- no hook activity appears in the log/state.

Check:

```bash
conductor hooks path
conductor hooks install
codex features list
```

Then:

1. confirm hooks are not explicitly disabled in Codex configuration;
2. enable goals when disabled: `codex features enable goals`;
3. restart the Codex CLI sessions;
4. open `/hooks` in each session;
5. review and trust the exact Conductor commands.

Codex stores trust against the hook definition hash. Reinstalling or moving the executable can require a new review.

## `/goal` is not available

Run:

```bash
codex features enable goals
```

Start a fresh Codex session and verify `/goal` appears in slash commands.

## `conductor goal` asks for permission or cannot reach tmux

Codex sandboxes generated shell commands. Conductor needs to write its small state directory and connect to the local tmux socket.

Use one of these approaches:

- approve the narrow `conductor` command when Codex requests it;
- configure an appropriate Codex permission profile;
- launch every session with the same temp-backed state:

```bash
export CONDUCTOR_HOME="${TMPDIR%/}/conductor"
mkdir -p "$CONDUCTOR_HOME" && chmod 700 "$CONDUCTOR_HOME"
conductor init
```

Do not give broad full-disk permission solely for Conductor.

## A hook points to an old binary

```bash
conductor hooks uninstall
conductor hooks install
```

Review/trust the updated command in `/hooks`.

The hooks installer backs up the previous file next to `hooks.json` before writing.

## Worker finished, but the local task is still running

Use explicit recovery so the exact message is unambiguous:

```bash
cat <<'HANDOFF' | conductor finish worker-1 --stdin --status complete
<copy Worker's final response here>
HANDOFF
```

`finish` can also use `--file path/to/handoff.md`.

With no explicit content, it tries the last assistant message cached from an earlier worker `Stop`; that can be an intermediate response, so explicit input is safer.

## Brain is visually idle, but the handoff remains queued

```bash
conductor idle
conductor flush
```

`idle` resets Conductor's local Brain activity state only. It does not touch Codex.

## `flush` says Brain is busy

Visually inspect the Brain terminal. When it is definitely at an empty composer:

```bash
conductor flush --force
```

Do not force delivery while Brain is generating or while you have an unsent draft in the composer.

## A handoff was pasted into the wrong pane

Conductor targets the active pane in the named session. Use one Codex pane per
Brain/Worker tmux session.

Inspect:

```bash
tmux list-panes -t project1--brain -F '#{pane_id} #{pane_active} #{pane_current_path} #{pane_current_command}'
tmux list-panes -t project1--worker-1 -F '#{pane_id} #{pane_active} #{pane_current_path} #{pane_current_command}'
```

Create separate tmux sessions rather than several Codex panes under one session name.

## Text appears in the composer but is not submitted

Older tmux versions may handle bracketed paste differently. Conductor falls back automatically when `paste-buffer -p` is unavailable.

Check tmux and retry:

```bash
tmux -V
conductor status
```

The tmux transport test suite covers multiline buffer loading and Enter submission, but terminal/TUI key handling can vary by version.

## Worker is stuck at `Replace goal?`

Codex asks for confirmation when a previous goal is still `active`, `paused`, `blocked`, usage-limited, or budget-limited. Conductor observes the visible tmux pane until it sees either an active turn or the exact replacement dialog, then confirms **Replace current goal** automatically. If input was already submitted but the result cannot be confirmed, Conductor reports an error and keeps the task active with `dispatch_state: uncertain`; this blocks unsafe retries until a later Codex hook or an explicit manual finish resolves it.

For a session already stuck on the dialog, press Enter once to accept the selected replacement. Alternatively press Escape, run `/goal clear`, and resend the assignment. Then verify the installed version:

```bash
conductor version
```

The timing knobs are `goal_prefix_delay_ms` and `goal_dispatch_timeout_ms` in `~/.conductor/config.json`. Increase the timeout only when a consistently slow machine still reproduces the issue.

## Worker receives literal `/goal ...` as an ordinary message

Conductor deliberately types `/goal ` first and pastes only the objective,
because a large whole-command paste can be treated as ordinary user text by
some Codex TUI versions. Confirm you are running a build containing this recovery logic or a current
source build, then retry after clearing the Worker composer.

```bash
conductor version
conductor status
```

Also verify that `/goal` is available in Worker's slash-command menu.

When Codex nevertheless handles the assignment as a normal prompt, the worker's `Stop` schedules one delayed local recheck. If no real goal ever appeared and Worker stayed idle, Brain receives the exact final response with status `implicit`. This in-hook check runs once; it does not poll Worker or consume model tokens.

## A worker session is missing

```bash
tmux list-sessions
```

Start the exact expected name:

```bash
tmux new -s project1--worker-1
```

Inside `project1--brain`, `conductor goal worker-1 ...` resolves to that physical
session. The default project still expects unprefixed `worker-1`. Run
`conductor project sessions project1` when unsure.

Worker aliases accept positive integer suffixes only.

## A handoff went to another project's Brain

This should not occur when all sessions follow the naming convention. Check
that Brain and Worker share the exact prefix before `--` and inspect all states:

```bash
tmux list-sessions
conductor status --all
```

Do not mix `project1--brain` with an unprefixed `worker-1`; that worker belongs to
the `default` project. The matching worker is `project1--worker-1`.

## A long goal is rejected

Conductor normally stores long objectives in a private file and sends a short path-based `/goal`. Confirm `inline_goal_max_chars` is no higher than 3900 in `~/.conductor/config.json`.

If Worker cannot read the private goal file under the active sandbox, either grant read access to the configured `CONDUCTOR_HOME` or place the detailed instructions in a file inside Worker's worktree and delegate a short goal that references it.

## Goal database or transcript errors

The primary completion path does not require transcript parsing. Database/transcript messages in `conductor.log` usually indicate that compatibility or one-shot reconciliation was attempted. Conductor refuses to infer an `implicit` completion when no local source is available or when a source returns an actual read/query error.

Use the explicit `finish` command rather than deleting Codex databases. Repair or move Codex state only when Codex's own diagnostics identify corruption.

## State appears corrupted

Back it up first:

```bash
cp -R ~/.conductor ~/.conductor.backup
```

Each project's `state.json` is human-readable. Goal and handoff files remain
in their own directories even if state needs manual repair.

For a clean Conductor reset, stop all related sessions, preserve any handoffs you need, then remove only `~/.conductor`. Re-run:

```bash
conductor init
```

Hook configuration is separate under `~/.codex/hooks.json`.
