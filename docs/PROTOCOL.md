# Sol–Luna protocol

## Roles

### Sol

Sol owns orchestration and judgment:

- receives the human's task;
- decides whether and what to delegate;
- authors each complete Luna `/goal`;
- selects the desired handoff depth and structure;
- waits without polling;
- reviews the returned worktree/handoff;
- decides whether to continue, redelegate, or ask the human.

### Luna

Each Luna owns one worktree and one active goal at a time:

- follows the exact goal Sol authored;
- validates its work;
- marks the built-in goal `complete` or `blocked` according to Codex's lifecycle;
- returns the handoff Sol requested.

### Conductor

Conductor owns only transport state:

- sends `/goal` to a named Luna terminal;
- observes lifecycle events and performs at most one local reconciliation for an ambiguous Stop;
- stores the final message exactly;
- delivers it to Sol when safe.

## Project scope

Each project contains one Sol and its own Luna pool. Physical tmux sessions
are namespaced (`project1--sol`, `project1--luna-1`), but Sol delegates with the
same logical alias used by the default project:

```bash
conductor goal luna-1 "..."
```

Conductor derives `project1` from the caller's tmux session and targets
`project1--luna-1`. Project prefixes are not added to the goal or final handoff.
Every project has an independent FIFO inbox and Sol busy/idle state.

## Delegation format

There is no model-facing JSON protocol. Sol uses a normal shell command:

```bash
cat <<'GOAL' | conductor goal luna-1 --stdin
<objective, constraints, validation, and desired handoff>
GOAL
```

Conductor sends the exact Sol-authored objective as a real goal:

```text
/goal <objective, constraints, validation, and desired handoff>
```

There is no appended relay sentence and no mandatory response schema. When an objective exceeds the configured inline limit, Conductor stores the original text privately and sends a short goal instructing Luna to read that file.

## Handoff envelope

Conductor adds only:

```text
[CONDUCTOR HANDOFF | <worker> | <goal-status>]
workspace: <absolute worker workspace>

<verbatim final assistant message>
```

The task id is deliberately omitted from model-visible text. It remains available in `conductor status --json` and local state.

The normal statuses are `complete` and `blocked`. `implicit` means the worker ended a normal Codex turn with a final message but no goal lifecycle was ever persisted, even after one delayed recheck of at least one readable local source. Sol must treat `implicit` as a recovery signal rather than proof of success and decide whether to inspect, retry, or ask the human.

## Handoff strategy belongs to Sol

A fixed schema would either waste tokens on simple tasks or lose information on complex ones. Sol should choose among patterns such as:

### Inspectable-code pattern

Use when changes are easy to inspect from the worktree:

```text
Return only tests/status, the relevant commit or worktree state, and facts I
cannot discover cheaply from git diff and the files themselves.
```

### Self-contained pattern

Use for research, architecture, debugging, or non-code work:

```text
Return a self-contained handoff with evidence, alternatives, trade-offs,
remaining uncertainty, and your recommendation.
```

### File-backed pattern

Use when evidence is large but Sol may not need all of it:

```text
Write the full report to <path>. In the final response include the conclusion,
critical caveats, and a recommendation on whether I need to read the full file.
```

### Comparative parallel pattern

Give several Luna sessions independent approaches, then let Sol compare their handoffs:

```bash
conductor goal luna-1 "Analyze approach A; do not modify code; return evidence."
conductor goal luna-2 "Analyze approach B independently; return evidence."
```

## Blocked workers

Conductor does not create a separate `BLOCKED` workflow. A built-in goal status of `blocked` is terminal for transport and produces a normal handoff.

Sol can then:

- answer the blocker with another goal;
- inspect the worker's workspace;
- delegate a different investigation;
- solve the issue itself;
- ask the human.

## Multiple workers

Every worker completion is delivered independently.

If Luna 2 finishes while Sol is handling Luna 1, Luna 2's message stays in the FIFO inbox. When Sol stops, the oldest pending message is delivered. This preserves visibility and prevents an active-turn steer.

This ordering applies only within one project. Completions in another project
are delivered independently to that project's Sol.

## Human intervention

The human can type in any terminal at any time. Conductor does not lock terminals.

Because paste safety is inferred from hooks, a human who bypasses or disables hooks should visually confirm terminal state before using `conductor flush --force`.
