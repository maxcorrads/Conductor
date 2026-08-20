#!/bin/sh
set -eu

worker="${1:-luna-1}"

cat <<'GOAL' | conductor goal "$worker" --stdin
Investigate the current failing authentication tests without modifying code.

Validate every claim against the repository. In your final response return a
self-contained handoff with:
- root cause;
- evidence and exact file paths;
- viable fixes and trade-offs;
- your recommendation;
- remaining uncertainty.
GOAL
