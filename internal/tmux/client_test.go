package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecClientSendsPromptThroughBuffer(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  -V) echo "tmux 3.4" ;;
  display-message) printf 'brain\t%%0\n' ;;
  list-panes) printf '%%1\tworker-1\t1\t/tmp/worktree\tcodex\n' ;;
  list-sessions) printf 'brain\nworker-1\n' ;;
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
  capture-pane) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
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
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{})
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
	want := []string{
		"capture-pane -p -J -t %1",
		"send-keys -t %1 C-u",
		"send-keys -t %1 -l /goal ",
	}
	if len(lines) < len(want)+3 {
		t.Fatalf("unexpected tmux calls: %s", calls)
	}
	for i, expected := range want {
		if lines[i] != expected {
			t.Fatalf("call %d\nwant: %q\n got: %q\nall: %s", i, expected, lines[i], calls)
		}
	}
	if !strings.HasPrefix(lines[3], "load-buffer ") || !strings.HasPrefix(lines[4], "paste-buffer ") {
		t.Fatalf("objective was not pasted after prefix: %s", calls)
	}
	if lines[5] != "send-keys -t %1 Enter" {
		t.Fatalf("Enter not sent last: %q", lines[5])
	}
}

func TestExecClientConfirmsReplaceGoalPromptAfterSubmit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  capture-pane)
    count=0
    if [ -f "$FAKE_TMUX_DIR/capture-count" ]; then count=$(cat "$FAKE_TMUX_DIR/capture-count"); fi
    count=$((count + 1))
    printf '%s' "$count" > "$FAKE_TMUX_DIR/capture-count"
    printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls"
    if [ "$count" -ge 2 ]; then
      printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n'
    fi
    ;;
  load-buffer) cat >/dev/null; printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		ReplaceProbeTimeout:  50 * time.Millisecond,
		ReplaceProbeInterval: time.Millisecond,
	})
	if err := client.SendGoal("%1", "new objective"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "send-keys -t %1 Enter\n"); got != 2 {
		t.Fatalf("expected goal submit and replacement confirmation; got %d\n%s", got, calls)
	}
}

func TestExecClientDismissesStaleReplacePromptBeforeDispatch(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  capture-pane) printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n'; printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  load-buffer) cat >/dev/null; printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{})
	client.sleep = func(time.Duration) {}
	if err := client.SendGoal("%1", "new objective"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) < 2 || lines[1] != "send-keys -t %1 Escape" {
		t.Fatalf("stale replacement prompt was not dismissed first: %s", calls)
	}
}

func TestReplaceGoalPromptDetectionRequiresExactMarkers(t *testing.T) {
	if !isReplaceGoalPrompt("Replace goal?\nReplace current goal\nSet the new objective and start it now\nKeep the current goal") {
		t.Fatal("expected exact replacement prompt to be recognized")
	}
	if isReplaceGoalPrompt("A log line mentioning Replace goal? only") {
		t.Fatal("partial text must not trigger automatic Enter")
	}
}

func TestResolvePane(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
if [ "$1" = list-panes ]; then
  printf '%%1\tworker-1\t0\t/tmp/one\tzsh\n%%2\tworker-1\t1\t/tmp/two\tcodex\n'
else
  exit 1
fi
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	pane, err := New(script).ResolvePane("worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if pane.ID != "%2" || pane.Path != "/tmp/two" {
		t.Fatalf("unexpected pane: %+v", pane)
	}
}
