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
	Bin string
}

func New(bin string) *ExecClient { return &ExecClient{Bin: bin} }

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

// SendGoal writes the slash-command prefix as literal key events, then pastes
// only the objective. Codex's TUI may collapse a large single paste into a
// placeholder before slash-command dispatch; keeping "/goal " outside the
// pasted block makes the command recognizable while preserving the objective
// verbatim, including newlines.
func (c *ExecClient) SendGoal(targetPane string, objective string) error {
	if strings.TrimSpace(objective) == "" {
		return errors.New("refusing to send an empty goal")
	}
	out, err := exec.Command(c.Bin, "send-keys", "-t", targetPane, "-l", "/goal ").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send /goal prefix: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := c.pasteAndSubmit(targetPane, objective); err != nil {
		// Best-effort cleanup so a partial /goal command is not left in the
		// composer after a transport failure.
		_ = exec.Command(c.Bin, "send-keys", "-t", targetPane, "C-u").Run()
		return err
	}
	return nil
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
