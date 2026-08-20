# Security model

## Local-only transport

Conductor contains no network client and makes no OpenAI API calls. It invokes only:

- the local `tmux` executable;
- the installed Conductor binary for lifecycle hooks and one-shot delivery;
- optionally `sqlite3` for a compatibility-only local goal-status lookup;
- `codex --version` and `codex features list` in `doctor`.

Codex itself continues to use the user's normal authenticated CLI subscription/session.

Codex sandboxing still applies to commands Sol or Luna execute. Depending on the selected permission profile, invoking `conductor goal` can require approval to write the state directory or access the tmux socket. Prefer a narrow permission or a shared temp-backed `CONDUCTOR_HOME`; do not disable the sandbox solely for this tool.

## Hook trust

Conductor installs command hooks in the user-level Codex hook configuration. Command hooks are executable code. Codex requires review and trust of their exact definition before execution.

Review the file and `/hooks` UI. The installer:

- preserves existing hook groups;
- writes a timestamped backup before changing an existing file;
- uses an absolute, shell-quoted Conductor path;
- removes only recognized Conductor handlers during uninstall;
- refuses malformed hook structures rather than overwriting them.

## Prompt-injection boundary

Luna's final message is model-authored content and may be influenced by untrusted repository text. Conductor pastes that content into Sol as a user message.

Sol must treat the handoff as untrusted worker output, not as higher-priority instructions. The Sol protocol should remain in trusted project/user instructions and retain decision authority.

Do not use Conductor as a security boundary between mutually untrusted agents or repositories.

## Workspace isolation

Use a separate git worktree for every Luna session. Conductor records the pane's current path but does not validate repository identity or prevent two workers from touching shared external files.

Worktrees isolate tracked working files, not necessarily:

- shared `.git` metadata;
- caches and build directories outside the worktree;
- package manager global state;
- credentials;
- local services or databases;
- arbitrary absolute paths.

Use Codex permissions/sandboxing appropriate to the task.

## Local file permissions

Conductor creates its home directories with `0700` and private files with `0600`. Stored data can include:

- complete delegated goals;
- final Luna handoffs;
- cached assistant messages;
- absolute paths;
- task and session identifiers;
- error logs.

Anyone with access to the user's account or backups may be able to read this data. Remove old data according to your own retention policy.

## tmux paste behavior

Conductor sends exact multiline text through a named tmux buffer and then sends Enter. It does not evaluate the text as a shell command itself. The receiving Codex model may subsequently choose to execute commands under its normal permission policy.

A wrong or stale target session can expose content in the wrong terminal. Keep
unique session names, use the same project prefix for a Sol and its workers,
and keep one active Codex pane per session. Project state is isolated, but all
projects still share the same local user and tmux server; project namespacing
is routing isolation, not a security sandbox.

## Executable replacement

Hooks contain the absolute Conductor executable path. Protect the installation directory from writes by less-trusted users. Replacing that binary changes what the trusted hook executes even if the textual hook definition remains unchanged.

The default `~/.local/bin` is appropriate for a single-user macOS account when its permissions are secure.
