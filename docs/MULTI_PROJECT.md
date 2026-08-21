# Multiple projects and multiple Brain sessions

Conductor 0.3 scopes every Brain and Worker pool to a **project ID**.
A project is a transport namespace, not a Codex concept and not necessarily a
single Git repository.

## Session naming

The backwards-compatible default project remains:

```text
brain
worker-1
worker-2
...
```

Named projects use:

```text
<project>--brain
<project>--worker-1
<project>--worker-2
...
```

Examples:

```text
project1--brain
project1--worker-1
project1--worker-2

project2--brain
project2--worker-1
```

Project names are normalized to lowercase. Use letters, numbers, and hyphens;
`--` is reserved for the separator.

## Initialize two projects

```bash
conductor project init project1
conductor project init project2
conductor project list
```

`project init` creates only local Conductor state. It does not start tmux,
Codex, Git, or worktrees.

## Open the terminals

```bash
# Project1 Brain
cd /repos/project1
tmux new -s project1--brain
codex

# Project1 Worker 1
cd /repos/project1-worker-1
tmux new -s project1--worker-1
codex

# Project2 Brain
cd /repos/project2
tmux new -s project2--brain
codex

# Project2 Worker 1
cd /repos/project2-worker-1
tmux new -s project2--worker-1
codex
```

## Delegate without repeating the project name

Inside `project1--brain`:

```bash
conductor goal worker-1 "Implement the project1 change."
```

Conductor resolves the physical target as `project1--worker-1`.

Inside `project2--brain`, the same command:

```bash
conductor goal worker-1 "Investigate the project2 issue."
```

resolves to `project2--worker-1`.

The short `worker-N` alias is what Brain sees and what appears in the handoff
envelope. The project prefix is transport metadata only, so namespacing does
not add text to goals or worker replies.

## Independent wake-up and queue behavior

Each project has its own:

- Brain busy/idle state;
- workers and active tasks;
- FIFO handoff inbox;
- task, cache, handoff, and log files;
- lock file.

A completed `project1--worker-1` can wake only `project1--brain`. It cannot wake,
queue behind, or change the state of `project2--brain`.

Workers still wake their own Brain one at a time as soon as they finish. If that
Brain is busy, only that project's handoff is queued.

## Commands outside tmux

When Conductor cannot infer a project from the current session, select one
explicitly:

```bash
conductor --project project1 status
conductor -p project2 inbox
conductor -p project1 flush
```

Inspect all initialized projects:

```bash
conductor project list
conductor status --all
conductor status --all --json
```

`CONDUCTOR_PROJECT=project1` can also provide a non-tmux default, but the
explicit flag is clearer for occasional administrative commands.

## Two Brain coordinators for the same repository

A project identifies an independent orchestration group, not a repository.
To run two Brain sessions against the same codebase, create two namespaces:

```text
project1-a--brain
project1-a--worker-1

project1-b--brain
project1-b--worker-1
```

Use separate Worker worktrees for every worker. Brain workspaces may point to the
same repository only when you deliberately accept concurrent coordinator
activity; separate Brain worktrees are safer if both may edit.

## State layout

The default project keeps the 0.1 layout. Named projects are isolated below
`projects/`:

```text
~/.conductor/
├── config.json
├── state.json
├── tasks/
├── handoffs/
├── cache/
├── logs/
└── projects/
    ├── project1/
    │   ├── state.json
    │   ├── tasks/
    │   ├── handoffs/
    │   ├── cache/
    │   └── logs/
    └── project2/
        └── ...
```

The hook configuration remains global. Every hook event is routed to the
owning project from its tmux session; if tmux context is unavailable,
Conductor falls back to recorded Codex session IDs and workspaces.

## Clean 0.3 runtime

The Brain/Worker protocol uses config and state schema 2. Version 1 runtime data
is rejected instead of being guessed or partially migrated. Move the previous
`~/.conductor` directory aside before installation, then initialize a clean
default project and any named projects you need.

## Current boundary

Each project has exactly one Brain session and any number of Worker sessions. To
obtain N Brain sessions, create N project namespaces. A Worker can have only one
active Conductor goal at a time.
