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

func TestExecClientReadsSessionCreationTimes(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tmux")
	body := `#!/bin/sh
case "$1" in
  list-sessions) printf 'demo--brain\t1787392800\ndemo--worker-1\t1787392810\ninvalid\tnot-a-time\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	created, err := New(script).SessionCreatedAt()
	if err != nil {
		t.Fatal(err)
	}
	if got := created["demo--brain"]; !got.Equal(time.Unix(1787392800, 0)) {
		t.Fatalf("Brain creation time = %v", got)
	}
	if got := created["demo--worker-1"]; !got.Equal(time.Unix(1787392810, 0)) {
		t.Fatalf("Worker creation time = %v", got)
	}
	if _, accepted := created["invalid"]; accepted {
		t.Fatal("invalid tmux creation time was accepted")
	}
}

func TestExecClientReadsActiveSessionPanesInOneQuery(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tmux")
	body := `#!/bin/sh
case "$1" in
  list-panes) printf '%%1\tdemo--brain\t0\t1\t/old\tcodex\n%%2\tdemo--brain\t1\t1\t/repo\tzsh\n%%3\tdemo--worker-1\t1\t1\t/worktree\tcodex-aarch64-apple-darwin\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	panes, err := New(script).SessionPanes()
	if err != nil {
		t.Fatal(err)
	}
	if panes["demo--brain"].ID != "%2" || panes["demo--brain"].Command != "zsh" || panes["demo--worker-1"].Command != "codex-aarch64-apple-darwin" {
		t.Fatalf("session panes = %+v", panes)
	}
}

func TestExecClientSendsGoalPrefixSeparatelyFromPastedObjective(t *testing.T) {
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
    if [ "$count" -ge 2 ]; then printf '• Working (1s • esc to interrupt)\n'; fi
    ;;
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
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		DispatchTimeout:       50 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
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
    if [ "$count" -eq 2 ]; then
      printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n'
    elif [ "$count" -ge 3 ]; then
      printf '• Working (1s • esc to interrupt)\n'
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
		DispatchTimeout:       50 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
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
  capture-pane)
    count=0
    if [ -f "$FAKE_TMUX_DIR/capture-count" ]; then count=$(cat "$FAKE_TMUX_DIR/capture-count"); fi
    count=$((count + 1))
    printf '%s' "$count" > "$FAKE_TMUX_DIR/capture-count"
    case "$count" in
      1|2|4) printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n' ;;
    esac
    printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls"
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
		DispatchTimeout:       50 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
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

func TestExecClientDoesNotTypeGoalUntilStaleDialogIsGone(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  capture-pane) printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n' ;;
  send-keys)
    if [ "$4" = Escape ]; then printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls"; else printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/unsafe"; fi
    ;;
  load-buffer|paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/unsafe" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		DispatchTimeout:       5 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
	err := client.SendGoal("%1", "must not be typed")
	if err == nil || !strings.Contains(err.Error(), "no new goal was typed") {
		t.Fatalf("expected stale-dialog timeout, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unsafe")); !os.IsNotExist(err) {
		t.Fatal("goal keys were sent before the stale dialog disappeared")
	}
}

func TestExecClientWaitsForDelayedReplaceGoalPrompt(t *testing.T) {
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
    if [ "$count" -eq 8 ]; then
      printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n'
    fi
    ;;
  load-buffer) cat >/dev/null ;;
  paste-buffer) ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		DispatchTimeout:       100 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
	if err := client.SendGoal("%1", "delayed replacement"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "send-keys -t %1 Enter\n"); got != 2 {
		t.Fatalf("expected submit and delayed replacement confirmation; got %d\n%s", got, calls)
	}
}

func TestExecClientFailsWhenGoalDispatchCannotBeConfirmed(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  capture-pane) ;;
  load-buffer) cat >/dev/null ;;
  paste-buffer) ;;
  send-keys) ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		DispatchTimeout:       5 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
	err := client.SendGoal("%1", "unconfirmed")
	if err == nil || !strings.Contains(err.Error(), "was not confirmed") {
		t.Fatalf("expected explicit dispatch failure, got %v", err)
	}
	if !IsGoalDispatchUncertain(err) {
		t.Fatalf("post-submit timeout was not classified uncertain: %T %v", err, err)
	}
}

func TestExecClientRefusesGoalWhileWorkerTurnIsActive(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := `#!/bin/sh
set -eu
case "$1" in
  capture-pane) printf '• Working (4s • esc to interrupt)\n' ;;
  send-keys|load-buffer|paste-buffer) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/mutations" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	err := New(script).SendGoal("%1", "must not be typed")
	if err == nil || !strings.Contains(err.Error(), "active Codex turn") {
		t.Fatalf("expected active-turn refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mutations")); !os.IsNotExist(err) {
		t.Fatal("goal dispatch mutated an active Worker pane")
	}
}

func TestExecClientFailsOnStaleDialogAboveAnActiveTurn(t *testing.T) {
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
    if [ "$count" -ge 2 ]; then
      printf 'Replace goal?\n1. Replace current goal  Set the new objective and start it now\n2. Cancel  Keep the current goal\n• Working (1s • esc to interrupt)\n'
    fi
    ;;
  load-buffer) cat >/dev/null ;;
  paste-buffer) ;;
  send-keys) printf '%s\n' "$*" >> "$FAKE_TMUX_DIR/calls" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_DIR", dir)
	client := NewWithGoalDispatchOptions(script, GoalDispatchOptions{
		DispatchTimeout:       50 * time.Millisecond,
		DispatchProbeInterval: time.Millisecond,
	})
	err := client.SendGoal("%1", "new objective")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous dispatch failure, got %v", err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "send-keys -t %1 Enter\n"); got != 1 {
		t.Fatalf("ambiguous dialog triggered an unsafe confirmation; Enter count=%d\n%s", got, calls)
	}
}

func TestReplaceGoalPromptDetectionRequiresExactMarkers(t *testing.T) {
	if !PaneShowsReplaceGoalPrompt("Replace goal?\nReplace current goal\nSet the new objective and start it now\nKeep the current goal") {
		t.Fatal("expected exact replacement prompt to be recognized")
	}
	if PaneShowsReplaceGoalPrompt("A log line mentioning Replace goal? only") {
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
