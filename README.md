<p align="center">
  <img src="docs/images/conductor-icon.png" width="144" height="144" alt="Conductor icon">
</p>

<h1 align="center">Conductor</h1>

<p align="center">
  A tiny, local message bus for visible Codex CLI sessions in real tmux terminals.
</p>

<p align="center">
  <a href="https://github.com/maxcorrads/Conductor/actions/workflows/ci.yml"><img src="https://github.com/maxcorrads/Conductor/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI status"></a>
  <img src="https://img.shields.io/badge/macOS-14%2B-000000?logo=apple&logoColor=white" alt="macOS 14 or newer">
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.22 or newer">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT License"></a>
</p>

Conductor is a tiny, local message bus for **visible Codex CLI sessions** running in real `tmux` terminals.

It lets any number of persistent **Sol** sessions coordinate their own **Luna**
worker pools without Codex's integrated subagent/spawn mechanism, API billing,
background polling, or an orchestration model. Each Sol/Luna group is an
isolated Conductor project.

```text
Project: project1                           Project: project2
┌─────────────────────────────────┐         ┌─────────────────────────────────┐
│ tmux: project1--sol             │         │ tmux: project2--sol             │
│             │                   │         │             │                   │
│             ├─ project1--luna-1 │         │             ├─ project2--luna-1 │
│             └─ project1--luna-2 │         │             └─ project2--luna-2 │
│                                 │         │                                 │
│ independent FIFO inbox          │         │ independent FIFO inbox          │
└─────────────────────────────────┘         └─────────────────────────────────┘

Inside either Sol terminal the command remains simply:

    conductor goal luna-1 "..."
```

Sol decides what to delegate, how Luna should work, and what kind of final handoff it wants. Conductor does not summarize or constrain the handoff. It forwards Luna's final assistant message verbatim, with only a small envelope identifying the worker, goal status, and workspace.

## Design goals

- **Minimum token overhead.** No polling turns, no orchestration LLM, no duplicated diffs, and only a tiny transport envelope.
- **Full visibility.** Goals and handoffs are pasted into real terminal windows that remain under human control.
- **Freedom of handoff.** Sol can request a commit-oriented response, a detailed technical handoff, research, alternatives, or a file-backed report.
- **Human control.** Conductor never compacts, restarts, resumes, selects models, manages context, or decides when to ask the human.
- **Event driven.** Codex lifecycle hooks signal activity and completion; there is no resident daemon and no polling loop.
- **Project isolation.** Every Sol has an independent worker namespace, state
  file, busy marker, task set, and handoff queue.

## What Conductor does not do

Conductor does **not**:

- create or manage git worktrees;
- choose Sol/Luna models;
- manage Codex authentication or subscriptions;
- compact, clear, restart, or resume a Codex context;
- use OpenAI APIs;
- run an LLM of its own;
- interpret whether a worker's result is good;
- require commits or a fixed handoff schema;
- use Codex subagents or `spawn_agent`.

## Requirements

- macOS on Apple Silicon or Intel;
- a recent official Codex CLI with the `goals` and `hooks` features;
- `tmux`;
- one visible Codex process per named tmux session;
- separate worktrees for Luna workers, created and managed by you.

Go is needed only to build from source. The release archive includes prebuilt macOS binaries.

## Install

From the extracted release directory:

```bash
./scripts/install.sh
```

The installer:

1. selects the bundled `darwin-arm64` or `darwin-amd64` binary;
2. installs it to `~/.local/bin/conductor` by default;
3. initializes `~/.conductor`;
4. merges Conductor's handlers into `~/.codex/hooks.json`, preserving existing hooks and writing a backup before changes.

Current Codex releases enable hooks by default. Enable goals only when `/goal` is missing, then validate the setup:

```bash
codex features enable goals
conductor doctor
```

Make sure `~/.local/bin` is on `PATH`. Restart newly opened Codex sessions after installing or changing hooks. In each Codex CLI, open `/hooks`, inspect the exact commands, and trust them; Codex intentionally skips untrusted command hooks.

Custom install directory:

```bash
CONDUCTOR_BIN_DIR="$HOME/bin" ./scripts/install.sh
```

Force a local source build instead of using the bundled binary:

```bash
CONDUCTOR_BUILD_FROM_SOURCE=1 ./scripts/install.sh
```

### Codex permissions

`conductor goal` writes private transport state outside the project and talks to the local tmux socket. Depending on the active Codex permission profile, the first delegation may require approval. For a low-friction ephemeral setup, launch every Sol/Luna terminal with the same temp-backed state directory:

```bash
export CONDUCTOR_HOME="${TMPDIR%/}/conductor"
mkdir -p "$CONDUCTOR_HOME" && chmod 700 "$CONDUCTOR_HOME"
conductor init
```

The state can be discarded after the sessions end. Keep the default `~/.conductor` when you prefer persistent diagnostics and explicitly grant the command the required local permission.

See [`docs/INSTALL.md`](docs/INSTALL.md) for the full walkthrough.

## Project namespaces

Conductor derives the project from the current tmux session name. A named
project uses this fixed convention:

```text
<project>--sol
<project>--luna-1
<project>--luna-2
...
```

For example, `project1--sol` and `project1--luna-1` belong to project
`project1`; `project2--sol` and `project2--luna-1` belong to project
`project2`. The two projects have separate state and queues.

The existing unprefixed sessions remain the backwards-compatible `default`
project:

```text
sol
luna-1
luna-2
...
```

Initialize a named project and inspect its expected sessions with:

```bash
conductor project init project1
conductor project sessions project1
conductor project list
```

Project names are normalized to lowercase and may contain letters, numbers,
and single hyphens. `--` is reserved as the session separator.

## Set up one named project

### 1. Sol

Open a real terminal window in the main workspace:

```bash
cd /path/to/project
tmux new -s project1--sol
codex
```

Choose Sol Max through the normal Codex model controls. Conductor does not touch model settings.

Give Sol the protocol once, or put the relevant instructions in your own project instructions:

```bash
conductor prompt sol
```

The published prompt is also in [`prompts/SOL.md`](prompts/SOL.md).

### 2. Luna workers

Create worktrees yourself. One possible layout is:

```bash
git worktree add ../project-luna-1 -b conductor/luna-1
git worktree add ../project-luna-2 -b conductor/luna-2
```

Open each worker in a separate real terminal window:

```bash
cd ../project-luna-1
tmux new -s project1--luna-1
codex
```

```bash
cd ../project-luna-2
tmux new -s project1--luna-2
codex
```

Choose Luna Max in each Codex process. The optional worker protocol is available with:

```bash
conductor prompt luna
```

Keep one Codex composer/pane per named tmux session. Although the physical
sessions are `project1--luna-1`, `project1--luna-2`, and so on, Sol still uses
the short logical aliases `luna-1`, `luna-2`, etc. Conductor adds the project
namespace locally and does not place it in the delegated goal.

## Run several projects at once

Create a second independent orchestra with another project name:

```bash
conductor project init project2

# Window 1
cd /path/to/project1
tmux new -s project1--sol
codex

# Window 2
cd /path/to/project1-luna-1
tmux new -s project1--luna-1
codex

# Window 3
cd /path/to/project2
tmux new -s project2--sol
codex

# Window 4
cd /path/to/project2-luna-1
tmux new -s project2--luna-1
codex
```

From `project1--sol`:

```bash
conductor goal luna-1 "Investigate the project1 task."
```

targets only `project1--luna-1`. The identical command from `project2--sol`
targets only `project2--luna-1`. A completion in one project wakes only that
project's Sol.

From a terminal that is not inside one of those tmux sessions, select the
project explicitly:

```bash
conductor --project project1 status
conductor -p project2 inbox
conductor status --all
```

To run two independent Sol coordinators against the same repository, use two
different project IDs, for example `project1-a` and `project1-b`, and give each
its own worker worktrees.

## Delegate from Sol

The safest form uses stdin, preserving multiline goals and avoiding shell quoting issues:

```bash
cat <<'GOAL' | conductor goal luna-1 --stdin
Investigate the authentication race without changing code.

In your final response give me a structured handoff containing:
- root cause;
- evidence with file paths;
- viable fixes and trade-offs;
- your recommendation.
GOAL
```

A short goal can be passed inline:

```bash
conductor goal luna-1 "Run the failing tests, identify the cause, and report the minimum useful handoff."
```

Delegate to several workers in the same Sol turn:

```bash
cat task-a.md | conductor goal luna-1 --stdin
cat task-b.md | conductor goal luna-2 --stdin
cat task-c.md | conductor goal luna-3 --stdin
```

After delegation, Sol should end its turn and remain idle. Conductor does not ask Sol to poll the workers.

## Handoffs remain free-form

Sol decides the output contract in each goal.

### Tiny implementation handoff

```text
Implement the fix and validate it. In the final response include only:
- commit or worktree state;
- tests run and result;
- information I cannot cheaply discover from the worktree.
```

### Detailed research handoff

```text
Do not modify code. Return a self-contained technical handoff with evidence,
alternatives, risks, and a recommended next step.
```

### File-backed handoff for lower context cost

```text
Write the full investigation to .conductor/auth-investigation.md in your
worktree. In the final response give me the conclusion, critical caveats,
and whether I should read the full file.
```

Conductor forwards the final response exactly as Luna produced it:

```text
[CONDUCTOR HANDOFF | luna-1 | complete]
workspace: /absolute/path/to/project-luna-1

<verbatim final Luna response>
```

The message is not summarized, restructured, truncated, or converted to a mandatory schema.

## Wake-up and queue behavior

Each worker wakes the Sol in its own project as soon as that worker finishes.

- If Sol is idle, the handoff is pasted after a short safety delay.
- If Sol is processing another handoff or a human prompt, the new handoff is queued.
- At the next Sol `Stop`, Conductor reserves the oldest queued handoff and pastes it after a short safety delay.
- If Sol starts another turn during that delay, the handoff is returned to the queue instead of being injected into the active turn.
- Remaining handoffs are delivered one at a time after subsequent Sol stops.
- Busy/idle state and FIFO ordering are scoped per project, so activity in
  `project1` cannot delay or wake `project2`.

There is no timer loop and no model polling. The only detached process is a one-shot delivery helper used after a Sol `Stop` so the terminal composer has time to become idle.

## How completion is detected

The primary path uses supported Codex hooks:

1. Luna receives a real `/goal`.
2. Codex calls `update_goal` with `complete` or `blocked`.
3. `PostToolUse` records the confirmed terminal goal status.
4. The following `Stop` supplies `last_assistant_message`.
5. Conductor stores that exact message and relays it to Sol.

`blocked` is terminal only from Conductor's transport perspective. Conductor does not interpret the blocker; Sol decides whether to send Luna another goal, investigate, use a different worker, or ask the human.

A transcript/goal-database reader exists only as a compatibility fallback. Codex documents transcript JSON as unstable, so manual recovery remains available.

## Long goals

Codex persists `/goal` objectives with a bounded size. Conductor sends goals
inline only up to the configured safe limit. Longer goals are written privately
under the selected project's task directory, and Luna receives a short `/goal`
that points to the absolute file path.

For inline goals, Conductor types the literal `/goal ` prefix first and pastes only the objective. This keeps large pastes on Codex's slash-command path instead of letting the entire command become an ordinary user prompt. The objective itself is not summarized or rewritten.

This keeps the persisted objective below the Codex limit without discarding the original instructions.

## CLI reference

```text
conductor [--project NAME] init
conductor goal <luna-N> [--stdin | --file PATH | OBJECTIVE]
conductor status [--json] [--all]
conductor inbox
conductor finish <luna-N> [--stdin | --file PATH] [--status STATUS]
conductor flush [--force]
conductor idle
conductor hooks install|uninstall|path
conductor prompt sol|luna
conductor doctor
conductor project init NAME
conductor project list
conductor project sessions NAME
conductor version
```

`--project NAME` (or `-p NAME`) selects a project explicitly. Inside a
recognized tmux session it is normally unnecessary because Conductor infers
the namespace automatically.

### `goal`

Creates one active local task and sends the exact Sol-authored objective as `/goal <objective>` to the selected Luna pane.

- `--stdin`: read the complete objective from stdin.
- `--file PATH`: read it from a file.

Conductor does not append a handoff schema or relay instruction. Sol owns the complete worker contract.

Only one active Conductor task is allowed per Luna session.

### `status`

Shows the current project's Sol state, recent tasks, workers, errors, and
pending handoffs. `--json` exposes that project's full local state;
`--all` shows every initialized project.

### `project`

- `project init NAME` creates isolated state for a named project and prints its
  session convention;
- `project list` lists initialized projects and Sol sessions;
- `project sessions NAME` prints the expected Sol/Luna names without changing
  agent state.

### `inbox`

Lists pending handoffs in FIFO order without delivering them.

### `finish`

Manual recovery when a Codex hook was unavailable or missed completion:

```bash
cat handoff.md | conductor finish luna-1 --stdin --status complete
```

When no explicit message is provided, Conductor tries the most recently cached assistant message for that active worker task. Passing `--stdin` or `--file` is safer when exact content matters.

### `flush`

Delivers the oldest pending handoff when Sol is idle. `--force` clears Conductor's local Sol-busy marker first; use it only after visually confirming that Sol is at an empty composer.

### `idle`

Resets only Conductor's local Sol activity/reservation state. It never changes Codex context or session state.

### `hooks`

Installs, removes, or prints the user-level Codex hooks file. Installation is additive and backed up. Uninstall removes only Conductor command handlers.

### `doctor`

Checks platform, binaries, feature flags, hook installation, and tmux sessions. It cannot verify hook trust; use `/hooks` inside Codex for that.

## Configuration

The generated config is `~/.conductor/config.json` and is shared by all
projects. See [`config.example.json`](config.example.json).

Important defaults:

```json
{
  "sol_session": "sol",
  "worker_session_pattern": "^luna-[1-9][0-9]*$",
  "delivery_delay_ms": 180,
  "inline_goal_max_chars": 3500,
  "terminal_goal_statuses": ["complete", "blocked"]
}
```

`sol_session` and `worker_session_pattern` configure the backwards-compatible
`default` project. Named projects always use `<project>--sol` and
`<project>--luna-N`, keeping routing deterministic without a registry.

`CONDUCTOR_HOME` can relocate all Conductor state for testing or isolation.

## Local data

By default Conductor writes only under `~/.conductor` and `~/.codex/hooks.json`:

```text
~/.conductor/
├── config.json
├── state.json                     # default project
├── state.lock
├── tasks/
│   └── <task-id>/goal.md
├── handoffs/
│   └── <message-id>.md
├── cache/
│   └── <task-id>-last-assistant.md
├── logs/
│   └── conductor.log
└── projects/
    ├── project1/
    │   ├── state.json
    │   ├── state.lock
    │   ├── tasks/
    │   ├── handoffs/
    │   ├── cache/
    │   └── logs/conductor.log
    └── project2/
        └── ...
```

Directories use mode `0700`; goal, state, handoff, cache, and log files use mode `0600` where Conductor creates them.

## Recovery and troubleshooting

Start with:

```bash
conductor doctor
conductor status
conductor inbox
```

Detailed recovery steps are in [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md).

Common recovery:

```bash
# Sol is visually idle, but Conductor believes it is busy
conductor idle
conductor flush

# A worker finished but the completion hook was missed
cat final-handoff.md | conductor finish luna-1 --stdin --status complete

# Hooks point to an old binary path
conductor hooks uninstall
conductor hooks install
```

## Security model

Conductor has no network client and no OpenAI API integration. It does execute through trusted Codex command hooks and paste model-authored text into another trusted local Codex session. Review [`docs/SECURITY.md`](docs/SECURITY.md) before using it with untrusted repositories or sensitive data.

## Development

```bash
make test
make vet
make race
make build
make dist
make package
```

The project is intentionally dependency-free beyond the Go standard library.

See:

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/MULTI_PROJECT.md`](docs/MULTI_PROJECT.md)
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)
- [`docs/COMPATIBILITY.md`](docs/COMPATIBILITY.md)
- [`CONTRIBUTING.md`](CONTRIBUTING.md)

## License

MIT. See [`LICENSE`](LICENSE).
