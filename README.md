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

---

Conductor lets any number of persistent **Brain** Codex sessions delegate work to their own **Worker** Codex sessions running in real tmux windows — without subagents, API billing, background polling, or an orchestration model. Goals are pasted into visible terminals, and Worker handoffs come back verbatim. Every Brain/Worker group is an isolated project.

```text
Project: project1                     Project: project2

  project1--brain                      project2--brain
        │                                    │
        ├─ project1--worker-1                ├─ project2--worker-1
        └─ project1--worker-2                └─ project2--worker-2

Each project has its own FIFO inbox, busy marker,
task set, and handoff queue.
```

Inside any Brain terminal, delegating is one command:

```bash
conductor goal worker-1 "..."
```

## Why

- **Minimum token overhead** — no polling turns, no orchestration LLM, no duplicated diffs, only a tiny transport envelope.
- **Full visibility** — goals and handoffs are pasted into real terminal windows under human control.
- **Free-form handoffs** — Brain defines the output contract per goal; Conductor forwards Worker's final response verbatim, never summarized or truncated.
- **Human control** — Conductor never compacts, restarts, resumes, selects models, or manages context.
- **Event driven** — Codex lifecycle hooks signal completion; bounded one-shot recovery replaces polling.
- **Project isolation** — independent namespaces, so activity in one project cannot wake or delay another.

What Conductor does **not** do: manage git worktrees, choose models, manage Codex auth or subscriptions, use OpenAI APIs, run an LLM of its own, judge results, or use Codex subagents. See [`docs/PROTOCOL.md`](docs/PROTOCOL.md).

## Requirements

- macOS on Apple Silicon or Intel;
- a recent official Codex CLI with the `goals` and `hooks` features;
- `tmux`;
- one visible Codex process per named tmux session;
- separate Worker worktrees, created and managed by you.

Go is needed only to build from source; release archives ship prebuilt binaries.

## Install

### CLI

Download and extract the latest archive from
[GitHub Releases](https://github.com/maxcorrads/Conductor/releases), then run:

```bash
./scripts/install.sh
```

The installer selects the bundled `darwin-arm64`/`darwin-amd64` binary, installs it to `~/.local/bin/conductor`, initializes `~/.conductor`, and merges Conductor's handlers into `~/.codex/hooks.json` (additive, with backup).

Then enable trust and validate:

```bash
codex features enable goals   # only if /goal is missing
conductor doctor
```

Make sure `~/.local/bin` is on your `PATH`. Restart Codex sessions after installing or changing hooks, open `/hooks` in each session, inspect the exact commands, and trust them — Codex skips untrusted command hooks.

Options:

```bash
CONDUCTOR_BIN_DIR="$HOME/bin" ./scripts/install.sh          # custom install dir
CONDUCTOR_BUILD_FROM_SOURCE=1 ./scripts/install.sh          # build from source
```

See [`docs/INSTALL.md`](docs/INSTALL.md) for the full walkthrough.

### Native macOS app (GUI)

Download `Conductor-vX.Y.Z-macos.zip` from the same GitHub Release, move `Conductor.app` to Applications, and open it. The notarized universal app runs on Apple Silicon and Intel Macs and requires macOS 14+.

On first launch, choose **Install CLI and hooks**: the app bundles the CLI and installs it to `~/.local/bin/conductor` plus the Codex hooks, preserving existing ones.

The app provides:

- a native project/Brain/Worker dashboard with menu bar status;
- exact goal dispatch and handoff inspection;
- bounded log tails and machine-readable doctor results;
- `idle`, `flush`, confirmed force-delivery, and confirmed manual finish;
- project initialization and additive hook management;
- configurable Terminal/iTerm2 priority for opening or attaching to tmux sessions;
- free-form Codex model selection plus model-aware reasoning effort when creating Brain/Worker sessions.

Details and privacy notes: [`docs/MACOS_APP.md`](docs/MACOS_APP.md).

The app is driven by the same machine-readable interface available to you on the command line:

```bash
conductor gui snapshot   # full dashboard state: projects, tasks, handoffs, logs (JSON)
conductor gui sessions   # tmux probe: live sessions, activity and attention flags (JSON)
conductor gui models     # Codex model catalog with supported reasoning levels (JSON)
```

### Codex permissions

`conductor goal` writes private transport state outside the project and talks to the local tmux socket. Depending on your Codex permission profile, the first delegation may require approval. For a low-friction ephemeral setup, launch every Brain/Worker terminal with the same temp-backed state directory:

```bash
export CONDUCTOR_HOME="${TMPDIR%/}/conductor"
mkdir -p "$CONDUCTOR_HOME" && chmod 700 "$CONDUCTOR_HOME"
conductor init
```

Keep the default `~/.conductor` if you prefer persistent diagnostics, then explicitly grant the required local permission.

## Quick start

### 1. Initialize the project

```bash
conductor project init myproject    # optional; omit for the `default` project
```

Sessions follow this fixed convention (named projects), keeping routing deterministic without a registry:

```text
myproject--brain     myproject--worker-1     myproject--worker-2 ...
```

Unprefixed sessions (`brain`, `worker-1`, ...) belong to the `default` project. Project names are lowercase letters, numbers, and single hyphens; `--` is reserved as separator.

### 2. Start the Brain

```bash
cd /path/to/project
tmux new -s myproject--brain
codex
```

Choose the model through normal Codex controls — Conductor never touches model settings. Give Brain the protocol once:

```bash
conductor prompt brain     # also published at prompts/BRAIN.md
```

### 3. Start Workers

Create worktrees yourself, one per worker:

```bash
git worktree add ../myproject-worker-1 -b conductor/worker-1
cd ../myproject-worker-1
tmux new -s myproject--worker-1
codex
```

Optionally load the worker protocol:

```bash
conductor prompt worker    # also published at prompts/WORKER.md
```

Keep one Codex composer per named tmux session. Brain uses short logical aliases (`worker-1`); Conductor adds the project namespace locally.

### 4. Delegate

The safest form uses stdin (multiline goals, no quoting issues):

```bash
cat <<'GOAL' | conductor goal worker-1 --stdin
Investigate the authentication race without changing code.

In your final response give me a structured handoff containing:
- root cause;
- evidence with file paths;
- viable fixes and trade-offs;
- your recommendation.
GOAL
```

Inline works for short goals, and several workers can be delegated in one Brain turn. After delegation Brain should end its turn — no polling needed.

Handoffs stay free-form; Brain decides the output contract. Worker's final response arrives verbatim:

```text
[CONDUCTOR HANDOFF | worker-1 | complete]
workspace: /absolute/path/to/myproject-worker-1

<verbatim final Worker response>
```

More delegation patterns: [`examples/delegate-structured.sh`](examples/delegate-structured.sh) and [`examples/delegate-file-backed.sh`](examples/delegate-file-backed.sh).

### Completion detection

Primary path via supported Codex hooks: Worker receives a real `/goal`, Codex reports `complete` or `blocked` through `update_goal`, `PostToolUse` records it, and the following `Stop` supplies the final assistant message, which Conductor relays to Brain. A bounded transcript/goal-database fallback relays with status `implicit` when a normal turn finished without an observed goal — a signal for Brain to verify, not proof of success. Manual recovery is always available with `conductor finish`. Details: [`docs/PROTOCOL.md`](docs/PROTOCOL.md).

Long goals above `inline_goal_max_chars` are written privately under the project's task directory; Worker receives a short `/goal` pointing at the absolute file path. Before assigning a worker, Conductor confirms Codex's native **Replace goal?** dialog when a previous persisted goal exists.

## Running multiple projects

Each named project is fully isolated — separate state, queues, and wake-ups:

```bash
conductor --project myproject status
conductor -p otherproject inbox
conductor status --all
```

From outside a recognized tmux session, select the project explicitly as shown above. See [`docs/MULTI_PROJECT.md`](docs/MULTI_PROJECT.md).

## CLI reference

```text
conductor [--project NAME] init
conductor goal <worker-N> [--stdin | --file PATH | OBJECTIVE]
conductor brain setup [--stdin | --file PATH | PROMPT]
conductor status [--json] [--all]
conductor inbox [--json]
conductor finish <worker-N> [--task-id ID] [--stdin | --file PATH] [--status STATUS]
conductor flush [--force]
conductor idle
conductor hooks install|uninstall|path
conductor prompt brain|worker
conductor doctor [--json]
conductor gui snapshot|sessions|models
conductor project init NAME
conductor projects                  # alias for `project list`
conductor project sessions NAME
conductor project delete NAME --yes
conductor version
```

Highlights:

- **`goal`** creates one active local task per Worker and sends the exact objective as `/goal <objective>` to the Worker pane. No schema or relay instruction is appended — Brain owns the full contract.
- **`brain setup`** pastes a setup prompt into the project's Brain only after confirming the pane is idle with an empty composer; it never clears an existing draft.
- **`status`** shows Brain state, recent tasks, workers, errors, and pending handoffs (`--json` for machine-readable output, `--all` for every initialized project).
- **`finish`** is manual recovery when a hook was unavailable: pipe a message or let Conductor reuse the cached last assistant message.
- **`flush [--force]`** delivers the oldest pending handoff when Brain is idle; `--force` clears the local busy marker first (only after visually confirming Brain is idle).
- **`idle`** resets only Conductor's local activity/reservation state — never Codex context.
- **`hooks install|uninstall|path`** manages the user-level `~/.codex/hooks.json`; installation is additive and backed up.
- **`doctor`** checks platform, binaries, feature flags, hook installation, and tmux sessions. It cannot verify hook trust; use `/hooks` inside Codex.
- **`project delete NAME --yes`** removes only the project's private runtime data — never workspaces or terminal sessions.
- **`gui`** emits the JSON snapshots consumed by the native app (schema 3); handy for scripts and dashboards too.

## Configuration

Generated at `~/.conductor/config.json`, shared by all projects. Full example: [`config.example.json`](config.example.json).

| Key | Default | Valid range | Description |
|---|---|---|---|
| `version` | `2` | `2` | Config/state schema version. |
| `brain_session` | `brain` | non-empty, no `--` | Brain tmux session name for the `default` project. |
| `worker_session_pattern` | `^worker-[1-9][0-9]*$` | valid regex | Worker session pattern for the `default` project; must not match named-project sessions. |
| `tmux_command` | `tmux` | non-empty | tmux binary to invoke. |
| `delivery_delay_ms` | `180` | 0–10000 | Safety delay before pasting into an idle Brain composer. |
| `goal_prefix_delay_ms` | `75` | 0–10000 | Delay between typing the `/goal ` prefix and pasting the objective. |
| `goal_dispatch_timeout_ms` | `10000` | 1000–60000 | How long dispatch waits for an observable Codex ack of `/goal`. |
| `goal_reconcile_delay_ms` | `1500` | 0–60000 | One-shot delayed recheck after an ambiguous worker Stop; cancelled by any later turn. |
| `inline_goal_max_chars` | `3500` | 256–3900 | Goals up to this size go inline; longer ones are file-backed. |
| `terminal_goal_statuses` | `["complete", "blocked"]` | non-empty list | Goal statuses treated as terminal. |
| `transcript_tail_bytes` | `33554432` | ≥ 1048576 | Maximum transcript tail read by the fallback reader. |

Named projects always use `<project>--brain` and `<project>--worker-N`, regardless of these settings. `CONDUCTOR_HOME` relocates all state for testing or isolation.

## Local data

By default Conductor writes only under `~/.conductor` and `~/.codex/hooks.json`:

```text
~/.conductor/
├── config.json
├── state.json                     # default project
├── state.lock
├── tasks/<task-id>/goal.md
├── handoffs/<message-id>.md
├── cache/<task-id>-last-assistant.md
├── logs/conductor.log
└── projects/<name>/               # same layout per named project
```

Directories use mode `0700`; files use `0600` where Conductor creates them.

## Recovery and troubleshooting

Start with:

```bash
conductor doctor && conductor status && conductor inbox
```

Common fixes:

```bash
# Brain is visually idle, but Conductor believes it is busy
conductor idle && conductor flush

# A worker finished but automatic/implicit recovery was unavailable
cat final-handoff.md | conductor finish worker-1 --stdin --status complete

# Hooks point to an old binary path
conductor hooks uninstall && conductor hooks install
```

Full guide: [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md).

## Security

Conductor has no network client and no OpenAI API integration. It executes through trusted Codex command hooks and pastes model-authored text into another trusted local Codex session. Review [`docs/SECURITY.md`](docs/SECURITY.md) before using it with untrusted repositories or sensitive data.

## Development

```bash
make test       # unit tests
make vet        # go vet
make race       # tests with race detector
make fmt-check  # gofmt check
make build      # local binary in build/
make dist       # darwin arm64+amd64 binaries in dist/
make release    # everything above plus packaging checks
```

The project is intentionally dependency-free beyond the Go standard library. Contributing guidelines: [`CONTRIBUTING.md`](CONTRIBUTING.md). Changelog: [`CHANGELOG.md`](CHANGELOG.md).

## Documentation

- [`docs/INSTALL.md`](docs/INSTALL.md) — full installation walkthrough
- [`docs/MACOS_APP.md`](docs/MACOS_APP.md) — native app behavior and signing
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — internal design
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md) — Brain/Worker protocol and completion detection
- [`docs/MULTI_PROJECT.md`](docs/MULTI_PROJECT.md) — multi-project isolation
- [`docs/COMPATIBILITY.md`](docs/COMPATIBILITY.md) — Codex compatibility matrix
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md) — recovery guide
- [`docs/SECURITY.md`](docs/SECURITY.md) — security model

## License

MIT. See [`LICENSE`](LICENSE).
