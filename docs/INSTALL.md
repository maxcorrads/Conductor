# Installation walkthrough

## 1. Install tmux

Use the package manager you normally trust on macOS, then verify:

```bash
tmux -V
```

## 2. Install Conductor

From the release directory:

```bash
./scripts/install.sh
```

Default destination:

```text
~/.local/bin/conductor
```

## 3. Enable goals when needed

Hooks are enabled by default in current Codex releases. When `/goal` is missing, run:

```bash
codex features enable goals
```

## 4. Review hooks

Start or restart a Codex CLI session, open `/hooks`, inspect all Conductor handlers, and trust them.

The configured commands should point to the installed absolute binary path and end with:

```text
hook session-start
hook user-prompt-submit
hook post-tool-use
hook stop
```

## 5. Validate

```bash
conductor doctor
```

`doctor` should report the Conductor hooks and Codex feature flags as enabled. It cannot inspect your trust decision.

### Optional: avoid repeated sandbox approvals

The default state directory is `~/.conductor`. A normal Codex workspace profile may ask before a generated command writes there or accesses tmux. One low-friction option is to use the system temp directory, which Codex workspace profiles normally expose as writable:

```bash
export CONDUCTOR_HOME="${TMPDIR%/}/conductor"
mkdir -p "$CONDUCTOR_HOME" && chmod 700 "$CONDUCTOR_HOME"
conductor init
```

Set the same environment variable before launching every Sol/Luna Codex
process across all projects. This state is intentionally disposable.
Alternatively, keep `~/.conductor` and approve or configure the narrow local
permission required by the `conductor` command.

## 6. Initialize a project

```bash
conductor project init project1
```

The default unprefixed project still works without this step. Named projects
use a deterministic tmux namespace and isolated state.

## 7. Start named sessions

```bash
cd /path/to/main-workspace
tmux new -s project1--sol
codex
```

In a separate worktree and terminal:

```bash
cd /path/to/luna-worktree
tmux new -s project1--luna-1
codex
```

Inside `project1--sol`, delegate with the short alias:

```bash
conductor goal luna-1 "..."
```

For a second simultaneous project, repeat with another prefix such as
`project2--sol` and `project2--luna-1`. See
[`MULTI_PROJECT.md`](MULTI_PROJECT.md).

## Upgrade

Run the new release installer. It replaces the binary and ensures the hook
definitions point to that installed path. Existing `sol`/`luna-N` state remains
the `default` project; no migration is required. Review `/hooks` again if Codex
marks the changed definition untrusted.

## Uninstall

Preserve local task/handoff state:

```bash
./scripts/uninstall.sh
```

Remove hooks, binary, and Conductor state:

```bash
./scripts/uninstall.sh --purge
```
