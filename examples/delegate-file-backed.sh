#!/bin/sh
set -eu

worker="${1:-worker-1}"

cat <<'GOAL' | conductor goal "$worker" --stdin
Perform the requested architecture investigation. Do not change production code.

Write the complete evidence and analysis to .conductor/architecture-handoff.md
inside your worktree. In the final response give Brain only the conclusion,
critical caveats, recommended next action, and whether the full file must be read.
GOAL
