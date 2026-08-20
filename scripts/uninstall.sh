#!/bin/sh
set -eu

PURGE=0
if [ "${1:-}" = "--purge" ]; then
  PURGE=1
  shift
fi
if [ "$#" -ne 0 ]; then
  echo "usage: $0 [--purge]" >&2
  exit 1
fi

BIN_DIR=${CONDUCTOR_BIN_DIR:-"$HOME/.local/bin"}
DEST="$BIN_DIR/conductor"
RUNNER=""

if [ -x "$DEST" ]; then
  RUNNER="$DEST"
elif command -v conductor >/dev/null 2>&1; then
  RUNNER=$(command -v conductor)
fi

if [ -n "$RUNNER" ]; then
  "$RUNNER" hooks uninstall || {
    echo "Warning: hook removal failed. Inspect ~/.codex/hooks.json manually." >&2
  }
fi

if [ -e "$DEST" ]; then
  rm -f "$DEST"
  echo "Removed $DEST"
else
  echo "No Conductor binary found at $DEST"
fi

if [ "$PURGE" -eq 1 ]; then
  HOME_DIR=${CONDUCTOR_HOME:-"$HOME/.conductor"}
  case "$HOME_DIR" in
    ""|"/")
      echo "Refusing to purge unsafe CONDUCTOR_HOME: $HOME_DIR" >&2
      exit 1
      ;;
  esac
  rm -rf "$HOME_DIR"
  echo "Removed state: $HOME_DIR"
else
  echo "Preserved local state. Re-run with --purge to remove ${CONDUCTOR_HOME:-$HOME/.conductor}."
fi
