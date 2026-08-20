# Multiple projects and multiple Sol sessions

Conductor 0.2 scopes every coordinator and worker pool to a **project ID**.
A project is a transport namespace, not a Codex concept and not necessarily a
single Git repository.

## Session naming

The backwards-compatible default project remains:

```text
sol
luna-1
luna-2
...
```

Named projects use:

```text
<project>--sol
<project>--luna-1
<project>--luna-2
...
```

Examples:

```text
project1--sol
project1--luna-1
project1--luna-2

project2--sol
project2--luna-1
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
# Project1 Sol
cd /repos/project1
tmux new -s project1--sol
codex

# Project1 Luna 1
cd /repos/project1-luna-1
tmux new -s project1--luna-1
codex

# Project2 Sol
cd /repos/project2
tmux new -s project2--sol
codex

# Project2 Luna 1
cd /repos/project2-luna-1
tmux new -s project2--luna-1
codex
```

## Delegate without repeating the project name

Inside `project1--sol`:

```bash
conductor goal luna-1 "Implement the project1 change."
```

Conductor resolves the physical target as `project1--luna-1`.

Inside `project2--sol`, the same command:

```bash
conductor goal luna-1 "Investigate the project2 issue."
```

resolves to `project2--luna-1`.

The short `luna-N` alias is what Sol sees and what appears in the handoff
envelope. The project prefix is transport metadata only, so namespacing does
not add text to goals or worker replies.

## Independent wake-up and queue behavior

Each project has its own:

- Sol busy/idle state;
- workers and active tasks;
- FIFO handoff inbox;
- task, cache, handoff, and log files;
- lock file.

A completed `project1--luna-1` can wake only `project1--sol`. It cannot wake,
queue behind, or change the state of `project2--sol`.

Workers still wake their own Sol one at a time as soon as they finish. If that
Sol is busy, only that project's handoff is queued.

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

## Two Sol coordinators for the same repository

A project identifies an independent orchestration group, not a repository.
To run two Sol sessions against the same codebase, create two namespaces:

```text
project1-a--sol
project1-a--luna-1

project1-b--sol
project1-b--luna-1
```

Use separate Luna worktrees for every worker. Sol workspaces may point to the
same repository only when you deliberately accept concurrent coordinator
activity; separate Sol worktrees are safer if both may edit.

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

## Compatibility with 0.1

Existing `sol` and `luna-N` sessions continue to use the `default` project and
the existing `~/.conductor/state.json`. No migration command is required.
You can add named projects incrementally while the default project remains in
use.

## Current boundary

Each project has exactly one Sol session and any number of Luna sessions. To
obtain N Sol sessions, create N project namespaces. A Luna can have only one
active Conductor goal at a time, exactly as in 0.1.
