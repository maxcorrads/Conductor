package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecClientSendsPromptThroughBuffer(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  -V) echo "tmux 3.4" ;;
  display-message) printf 'sol\t%%0\n' ;;
  list-panes) printf '%%1\tluna-1\t1\t/tmp/worktree\tcodex\n' ;;
  list-sessions) printf 'sol\nluna-1\n' ;;
  has-session) exit 0 ;;
  load-buffer) cat > "$FAKE_TMUX_DIR/payload"; printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := New(script)
	prompt := "/goal line one\nline two"
	if err := client.SendPrompt("%1", prompt); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != prompt {
		t.Fatalf("payload changed: %q", payload)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !strings.Contains(string(calls), "send-keys -t %1 Enter") {
		t.Fatalf("Enter not sent: %s", calls)
	}
}

func TestExecClientSendsGoalPrefixSeparatelyFromPastedObjective(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  load-buffer) cat > "$FAKE_TMUX_DIR/payload"; printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := New(script)
	objective := "line one\nline two\n" + strings.Repeat("x", 1_200)
	if err := client.SendGoal("%1", objective); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != objective {
		t.Fatalf("objective changed: %q", payload)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) < 4 {
		t.Fatalf("unexpected tmux calls: %s", calls)
	}
	if lines[0] != "send-keys -t %1 -l /goal " {
		t.Fatalf("goal prefix was not sent separately: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "load-buffer ") || !strings.HasPrefix(lines[2], "paste-buffer ") {
		t.Fatalf("objective was not pasted after prefix: %s", calls)
	}
	if lines[3] != "send-keys -t %1 Enter" {
		t.Fatalf("Enter not sent last: %q", lines[3])
	}
}

func TestResolvePane(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
if [ "$1" = list-panes ]; then
  printf '%%1\tluna-1\t0\t/tmp/one\tzsh\n%%2\tluna-1\t1\t/tmp/two\tcodex\n'
else
  exit 1
fi
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	pane, err := New(script).ResolvePane("luna-1")
	if err != nil {
		t.Fatal(err)
	}
	if pane.ID != "%2" || pane.Path != "/tmp/two" {
		t.Fatalf("unexpected pane: %+v", pane)
	}
}
