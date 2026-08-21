# Contributing

Conductor is intentionally small. Changes should preserve its core constraint: it is a deterministic transport, not an intelligent orchestrator.

## Principles

- Do not add an LLM, API call, daemon, or polling loop.
- Do not summarize or reinterpret Worker's handoff.
- Do not manage model selection, context, compaction, Git, or worktrees.
- Keep model-visible transport text minimal.
- Prefer supported Codex lifecycle inputs over transcript parsing.
- Keep transcript/database parsing conservative and fallback-only.
- Preserve existing user hooks during install and uninstall.

## Local checks

```bash
make fmt-check
make test
make vet
make race
make build
```

## Pull requests

Include:

- the failure mode or use case;
- why the change belongs in a transport layer;
- tests for state/race behavior;
- any additional model-visible tokens introduced by the change.
