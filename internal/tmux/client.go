package tmux

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Pane struct {
	ID      string
	Session string
	Path    string
	Command string
	Active  bool
}

type Client interface {
	CurrentSession() (string, string, error)
	ResolvePane(session string) (Pane, error)
	SessionExists(session string) bool
	ListSessions() ([]string, error)
	SendGoal(targetPane string, objective string) error
	SendPrompt(targetPane string, prompt string) error
	Version() (string, error)
}

type ExecClient struct {
	Bin          string
	goalDispatch GoalDispatchOptions
	sleep        func(time.Duration)
}

type GoalDispatchOptions struct {
	PrefixDelay           time.Duration
	DispatchTimeout       time.Duration
	DispatchProbeInterval time.Duration
}

// GoalDispatchUncertainError means Conductor submitted input to the pane but
// could not prove the resulting Codex state. Callers must retain an active task
// and block retries until a later hook or explicit user action resolves it.
type GoalDispatchUncertainError struct {
	Err error
}

func (e *GoalDispatchUncertainError) Error() string { return e.Err.Error() }
func (e *GoalDispatchUncertainError) Unwrap() error { return e.Err }

func IsGoalDispatchUncertain(err error) bool {
	var uncertain *GoalDispatchUncertainError
	return errors.As(err, &uncertain)
}

func DefaultGoalDispatchOptions() GoalDispatchOptions {
	return GoalDispatchOptions{
		PrefixDelay:           75 * time.Millisecond,
		DispatchTimeout:       10 * time.Second,
		DispatchProbeInterval: 75 * time.Millisecond,
	}
}

func New(bin string) *ExecClient {
	return NewWithGoalDispatchOptions(bin, DefaultGoalDispatchOptions())
}

func NewWithGoalDispatchOptions(bin string, options GoalDispatchOptions) *ExecClient {
	if options.DispatchProbeInterval <= 0 {
		options.DispatchProbeInterval = 75 * time.Millisecond
	}
	return &ExecClient{Bin: bin, goalDispatch: options, sleep: time.Sleep}
}

func (c *ExecClient) Version() (string, error) {
	out, err := exec.Command(c.Bin, "-V").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s -V: %w: %s", c.Bin, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *ExecClient) CurrentSession() (string, string, error) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" && os.Getenv("TMUX") == "" {
		return "", "", errors.New("not running inside tmux")
	}
	args := []string{"display-message", "-p", "#{session_name}\t#{pane_id}"}
	if pane != "" {
		args = []string{"display-message", "-p", "-t", pane, "#{session_name}\t#{pane_id}"}
	}
	out, err := exec.Command(c.Bin, args...).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("resolve current tmux session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", fmt.Errorf("unexpected tmux response %q", strings.TrimSpace(string(out)))
	}
	return parts[0], parts[1], nil
}

func (c *ExecClient) ResolvePane(session string) (Pane, error) {
	format := "#{pane_id}\t#{session_name}\t#{pane_active}\t#{pane_current_path}\t#{pane_current_command}"
	out, err := exec.Command(c.Bin, "list-panes", "-t", session, "-F", format).CombinedOutput()
	if err != nil {
		return Pane{}, fmt.Errorf("find tmux session %q: %w: %s", session, err, strings.TrimSpace(string(out)))
	}
	var fallback *Pane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) != 5 {
			continue
		}
		pane := Pane{ID: parts[0], Session: parts[1], Active: parts[2] == "1", Path: parts[3], Command: parts[4]}
		if fallback == nil {
			copy := pane
			fallback = &copy
		}
		if pane.Active {
			return pane, nil
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Pane{}, fmt.Errorf("tmux session %q has no panes", session)
}

func (c *ExecClient) SessionExists(session string) bool {
	return exec.Command(c.Bin, "has-session", "-t", session).Run() == nil
}

func (c *ExecClient) ListSessions() ([]string, error) {
	out, err := exec.Command(c.Bin, "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no server running") || strings.Contains(string(out), "failed to connect") {
			return nil, nil
		}
		return nil, fmt.Errorf("list tmux sessions: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

func (c *ExecClient) SendPrompt(targetPane string, prompt string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("refusing to send an empty prompt")
	}
	return c.pasteAndSubmit(targetPane, prompt)
}

// SendGoal writes the slash-command prefix as literal key events and then
// pastes only the objective. Codex's TUI may collapse a large single paste into a placeholder
// before slash-command dispatch; keeping "/goal " outside the pasted block
// makes the command recognizable while preserving the objective verbatim,
// including newlines.
//
// A bounded, local capture-pane probe confirms Codex's "Replace goal?" dialog
// when a previous persisted goal exists. Dispatch succeeds only after the pane
// shows an active turn or a confirmed replacement dialog disappears. It never
// talks to a model and fails closed when the timeout expires.
func (c *ExecClient) SendGoal(targetPane string, objective string) error {
	if strings.TrimSpace(objective) == "" {
		return errors.New("refusing to send an empty goal")
	}

	// Refuse to type into an active turn. Besides protecting unrelated work,
	// this makes a later Working marker an acknowledgement of this dispatch
	// rather than stale evidence from a preceding turn.
	visible, err := c.captureVisiblePane(targetPane)
	if err != nil {
		return fmt.Errorf("inspect Worker pane before goal dispatch: %w", err)
	}
	// Recover a pane left at the replacement prompt by an earlier failed
	// dispatch before attempting another slash command.
	if PaneShowsReplaceGoalPrompt(visible) && PaneShowsActiveTurn(visible) {
		return errors.New("refusing to dispatch a goal while the Worker pane has ambiguous Working and Replace goal states")
	}
	if PaneShowsReplaceGoalPrompt(visible) {
		if err := c.sendKey(targetPane, "Escape"); err != nil {
			return err
		}
		if err := c.waitForReplaceGoalPromptDismissal(targetPane); err != nil {
			return err
		}
	} else if PaneShowsActiveTurn(visible) {
		return errors.New("refusing to dispatch a goal while the Worker has an active Codex turn")
	}

	// Start from an empty composer. Preserve Codex's persisted-goal lifecycle:
	// replacement is confirmed through its native dialog instead of clearing the
	// existing goal implicitly before the new assignment is acknowledged.
	if err := c.sendKey(targetPane, "C-u"); err != nil {
		return err
	}
	if err := c.sendLiteral(targetPane, "/goal "); err != nil {
		return fmt.Errorf("tmux send /goal prefix: %w", err)
	}
	c.pause(c.goalDispatch.PrefixDelay)
	if err := c.pasteAndSubmit(targetPane, objective); err != nil {
		// Best-effort cleanup so a partial /goal command is not left in the
		// composer after a transport failure.
		_ = exec.Command(c.Bin, "send-keys", "-t", targetPane, "C-u").Run()
		return &GoalDispatchUncertainError{Err: err}
	}
	if err := c.confirmGoalDispatch(targetPane); err != nil {
		return &GoalDispatchUncertainError{Err: err}
	}
	return nil
}

func (c *ExecClient) sendLiteral(targetPane, text string) error {
	out, err := exec.Command(c.Bin, "send-keys", "-t", targetPane, "-l", text).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send literal keys: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *ExecClient) sendKey(targetPane, key string) error {
	out, err := exec.Command(c.Bin, "send-keys", "-t", targetPane, key).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *ExecClient) pause(delay time.Duration) {
	if delay > 0 && c.sleep != nil {
		c.sleep(delay)
	}
}

func (c *ExecClient) captureVisiblePane(targetPane string) (string, error) {
	out, err := exec.Command(c.Bin, "capture-pane", "-p", "-J", "-t", targetPane).CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	// -J is available on supported tmux versions, but retry without it so the
	// recovery path remains harmless on older installations.
	fallback, fallbackErr := exec.Command(c.Bin, "capture-pane", "-p", "-t", targetPane).CombinedOutput()
	if fallbackErr != nil {
		return "", fmt.Errorf("tmux capture pane: %w: %s", fallbackErr, strings.TrimSpace(string(fallback)))
	}
	return string(fallback), nil
}

func PaneShowsReplaceGoalPrompt(pane string) bool {
	return strings.Contains(pane, "Replace goal?") &&
		strings.Contains(pane, "Replace current goal") &&
		strings.Contains(pane, "Set the new objective and start it now") &&
		strings.Contains(pane, "Keep the current goal")
}

func (c *ExecClient) waitForReplaceGoalPromptDismissal(targetPane string) error {
	timeout := c.goalDispatch.DispatchTimeout
	if timeout <= 0 {
		return errors.New("goal dispatch confirmation is disabled")
	}
	deadline := time.Now().Add(timeout)
	for {
		visible, err := c.captureVisiblePane(targetPane)
		if err != nil {
			return fmt.Errorf("confirm stale Replace goal dialog was dismissed: %w", err)
		}
		replaceVisible := PaneShowsReplaceGoalPrompt(visible)
		active := PaneShowsActiveTurn(visible)
		if replaceVisible && active {
			return errors.New("Worker pane has ambiguous Working and Replace goal states after dismissing the stale dialog")
		}
		if !replaceVisible {
			if active {
				return errors.New("Worker started an active Codex turn while dismissing the stale Replace goal dialog")
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("stale Replace goal dialog did not close within %s; no new goal was typed", timeout)
		}
		c.pause(c.goalDispatch.DispatchProbeInterval)
	}
}

func PaneShowsActiveTurn(content string) bool {
	busy := false
	for _, line := range strings.Split(strings.ToLower(content), "\n") {
		line = codexStatusLine(line)
		working := strings.HasPrefix(line, "working")
		if working && (strings.Contains(line, "esc to interrupt") || strings.Contains(line, "esc to cancel")) {
			busy = true
			continue
		}
		if strings.HasPrefix(line, "worked for") ||
			strings.HasPrefix(line, "goal paused") ||
			strings.HasPrefix(line, "goal stalled") ||
			strings.HasPrefix(line, "conversation interrupted") {
			busy = false
		}
	}
	return busy
}

func codexStatusLine(line string) string {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"•", "—", "■", "▪", "●"} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return line
}

func (c *ExecClient) confirmGoalDispatch(targetPane string) error {
	timeout := c.goalDispatch.DispatchTimeout
	if timeout <= 0 {
		return errors.New("goal dispatch confirmation is disabled")
	}
	deadline := time.Now().Add(timeout)
	replacementConfirmed := false
	var lastCaptureErr error
	for {
		visible, err := c.captureVisiblePane(targetPane)
		if err != nil {
			lastCaptureErr = err
		} else {
			lastCaptureErr = nil
			replaceVisible := PaneShowsReplaceGoalPrompt(visible)
			active := PaneShowsActiveTurn(visible)
			if replaceVisible && active {
				return errors.New("goal dispatch acknowledgement is ambiguous: Worker pane shows both Working and Replace goal")
			}
			if active {
				return nil
			}
			if replaceVisible {
				if !replacementConfirmed {
					if err := c.sendKey(targetPane, "Enter"); err != nil {
						return err
					}
					replacementConfirmed = true
					// Always allow a post-Enter observation, even when the dialog
					// appeared at the edge of the original dispatch deadline.
					confirmationGrace := timeout
					if confirmationGrace > 2*time.Second {
						confirmationGrace = 2 * time.Second
					}
					deadline = time.Now().Add(confirmationGrace)
				}
			} else if replacementConfirmed {
				return nil
			}
		}
		if !time.Now().Before(deadline) {
			if lastCaptureErr != nil {
				return fmt.Errorf("goal dispatch was not confirmed within %s: %w", timeout, lastCaptureErr)
			}
			return fmt.Errorf("goal dispatch was not confirmed within %s; inspect the Worker terminal before retrying", timeout)
		}
		c.pause(c.goalDispatch.DispatchProbeInterval)
	}
}

func (c *ExecClient) pasteAndSubmit(targetPane string, text string) error {
	bufferName := "conductor-" + randomHex(8)
	load := exec.Command(c.Bin, "load-buffer", "-b", bufferName, "-")
	load.Stdin = strings.NewReader(text)
	var loadErr bytes.Buffer
	load.Stderr = &loadErr
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w: %s", err, strings.TrimSpace(loadErr.String()))
	}

	pasteArgs := []string{"paste-buffer", "-p", "-d", "-b", bufferName, "-t", targetPane}
	out, err := exec.Command(c.Bin, pasteArgs...).CombinedOutput()
	if err != nil {
		// Older tmux releases do not support bracketed-paste mode (-p).
		pasteArgs = []string{"paste-buffer", "-d", "-b", bufferName, "-t", targetPane}
		out, err = exec.Command(c.Bin, pasteArgs...).CombinedOutput()
		if err != nil {
			_ = exec.Command(c.Bin, "delete-buffer", "-b", bufferName).Run()
			return fmt.Errorf("tmux paste-buffer: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	out, err = exec.Command(c.Bin, "send-keys", "-t", targetPane, "Enter").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send Enter: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return hex.EncodeToString(buf)
}
