package hookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Path    string
	Backup  string
	Changed bool
}

func CodexHooksPath() (string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "hooks.json"), nil
}

func Install(executable string) (Result, error) {
	path, err := CodexHooksPath()
	if err != nil {
		return Result{}, err
	}
	root, err := loadRoot(path)
	if err != nil {
		return Result{}, err
	}
	changed := false
	for _, spec := range []struct {
		event   string
		matcher string
		command string
		timeout int
	}{
		{"SessionStart", "", shellQuote(executable) + " hook session-start", 3},
		{"UserPromptSubmit", "", shellQuote(executable) + " hook user-prompt-submit", 3},
		{"PostToolUse", "^update_goal$", shellQuote(executable) + " hook post-tool-use", 3},
		{"Stop", "", shellQuote(executable) + " hook stop", 10},
	} {
		added, addErr := addHandler(root, spec.event, spec.matcher, spec.command, spec.timeout)
		if addErr != nil {
			return Result{}, addErr
		}
		if added {
			changed = true
		}
	}
	if _, ok := root["description"]; !ok {
		root["description"] = "Conductor lifecycle hooks for visible Codex CLI sessions in tmux."
		changed = true
	}
	if !changed {
		return Result{Path: path, Changed: false}, nil
	}
	backup, err := saveRoot(path, root)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: path, Backup: backup, Changed: true}, nil
}

func Uninstall() (Result, error) {
	path, err := CodexHooksPath()
	if err != nil {
		return Result{}, err
	}
	root, err := loadRoot(path)
	if errors.Is(err, os.ErrNotExist) {
		return Result{Path: path}, nil
	}
	if err != nil {
		return Result{}, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return Result{Path: path}, nil
	}
	changed := false
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop"} {
		groups, groupsErr := eventGroups(hooks, event)
		if groupsErr != nil {
			return Result{}, groupsErr
		}
		var keptGroups []any
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, rawGroup)
				continue
			}
			handlers, _ := group["hooks"].([]any)
			var keptHandlers []any
			for _, rawHandler := range handlers {
				handler, ok := rawHandler.(map[string]any)
				if !ok {
					keptHandlers = append(keptHandlers, rawHandler)
					continue
				}
				command, _ := handler["command"].(string)
				if isConductorCommand(command, event) {
					changed = true
					continue
				}
				keptHandlers = append(keptHandlers, rawHandler)
			}
			if len(keptHandlers) > 0 {
				group["hooks"] = keptHandlers
				keptGroups = append(keptGroups, group)
			}
		}
		if len(keptGroups) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keptGroups
		}
	}
	if !changed {
		return Result{Path: path}, nil
	}
	backup, err := saveRoot(path, root)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: path, Backup: backup, Changed: true}, nil
}

func Installed(executable string) (bool, string, error) {
	path, err := CodexHooksPath()
	if err != nil {
		return false, "", err
	}
	root, err := loadRoot(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, path, nil
	}
	if err != nil {
		return false, path, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	for _, spec := range []struct {
		event, matcher string
	}{
		{"SessionStart", ""},
		{"UserPromptSubmit", ""},
		{"PostToolUse", "^update_goal$"},
		{"Stop", ""},
	} {
		if !containsHandler(hooks, spec.event, spec.matcher, executable) {
			return false, path, nil
		}
	}
	return true, path, nil
}

func loadRoot(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"hooks": map[string]any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	rawHooks, exists := root["hooks"]
	if !exists {
		root["hooks"] = map[string]any{}
	} else if _, ok := rawHooks.(map[string]any); !ok {
		return nil, fmt.Errorf("parse %s: top-level hooks must be a JSON object", path)
	}
	return root, nil
}

func saveRoot(path string, root map[string]any) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	backup := ""
	if data, err := os.ReadFile(path); err == nil {
		backup = path + ".bak-" + time.Now().Format("20060102-150405.000000000")
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return "", fmt.Errorf("write backup: %w", err)
		}
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hooks-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return backup, nil
}

func addHandler(root map[string]any, event, matcher, command string, timeout int) (bool, error) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	if containsHandler(hooks, event, matcher, command) {
		return false, nil
	}
	groups, err := eventGroups(hooks, event)
	if err != nil {
		return false, err
	}
	handler := map[string]any{"type": "command", "command": command, "timeout": timeout}
	group := map[string]any{"hooks": []any{handler}}
	if matcher != "" {
		group["matcher"] = matcher
	}
	hooks[event] = append(groups, group)
	return true, nil
}

func eventGroups(hooks map[string]any, event string) ([]any, error) {
	raw, exists := hooks[event]
	if !exists || raw == nil {
		return nil, nil
	}
	groups, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("hooks.%s must be a JSON array", event)
	}
	return groups, nil
}

func containsHandler(hooks map[string]any, event, expectedMatcher, executableOrCommand string) bool {
	groups, _ := hooks[event].([]any)
	for _, rawGroup := range groups {
		group, _ := rawGroup.(map[string]any)
		matcher, _ := group["matcher"].(string)
		if matcher != expectedMatcher {
			continue
		}
		handlers, _ := group["hooks"].([]any)
		for _, rawHandler := range handlers {
			handler, _ := rawHandler.(map[string]any)
			command, _ := handler["command"].(string)
			if command == executableOrCommand {
				return true
			}
			if executable, ok := conductorCommandExecutable(command, event); ok && executable == executableOrCommand {
				return true
			}
		}
	}
	return false
}

func isConductorCommand(command, event string) bool {
	_, ok := conductorCommandExecutable(command, event)
	return ok
}

func conductorCommandExecutable(command, event string) (string, bool) {
	subcommand := map[string]string{
		"SessionStart":     "hook session-start",
		"UserPromptSubmit": "hook user-prompt-submit",
		"PostToolUse":      "hook post-tool-use",
		"Stop":             "hook stop",
	}[event]
	if subcommand == "" {
		return "", false
	}
	trimmed := strings.TrimSpace(command)
	suffix := " " + subcommand
	if !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
	executable, ok := unquoteExecutableToken(token)
	if !ok || filepath.Base(executable) != "conductor" {
		return "", false
	}
	return executable, true
}

func unquoteExecutableToken(token string) (string, bool) {
	if len(token) >= 2 && token[0] == '\'' && token[len(token)-1] == '\'' {
		inner := token[1 : len(token)-1]
		withoutEscapedQuotes := strings.ReplaceAll(inner, "'\\''", "")
		if strings.Contains(withoutEscapedQuotes, "'") {
			return "", false
		}
		return strings.ReplaceAll(inner, "'\\''", "'"), true
	}
	if token == "" || strings.ContainsAny(token, " \t\r\n;&|$`()<>\\\"'") {
		return "", false
	}
	return token, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
