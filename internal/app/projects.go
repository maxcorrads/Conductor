package app

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/project"
	"github.com/maxcorrads/conductor/internal/state"
)

// DiscoverProjectIDs returns every project with local state, plus the default
// project for backwards compatibility. Named projects are represented by
// private directories under ~/.conductor/projects/<id>.
func DiscoverProjectIDs() ([]string, error) {
	base, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	ids := []string{project.DefaultID}
	entries, err := os.ReadDir(base.ProjectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return ids, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, normalizeErr := project.NormalizeID(entry.Name())
		if normalizeErr != nil || id == project.DefaultID {
			continue
		}
		paths := config.ForProject(base, id)
		if _, statErr := os.Stat(paths.State); statErr == nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids[1:])
	return ids, nil
}

// NewForHook routes a global Codex hook event to the project that owns the
// current tmux session. If tmux context is unavailable, it falls back to exact
// Codex session IDs and finally the most specific recorded workspace path.
func NewForHook(input HookInput) (*App, error) {
	inferred, err := New()
	if err != nil {
		return nil, err
	}
	if session, _, currentErr := inferred.Tmux.CurrentSession(); currentErr == nil {
		if parsed, ok := project.ParseSession(session, inferred.Config.SolSession, inferred.Config.WorkerSessionPattern); ok {
			if parsed.ProjectID == inferred.ProjectID {
				return inferred, nil
			}
			return NewForProject(parsed.ProjectID)
		}
	}

	ids, err := DiscoverProjectIDs()
	if err != nil {
		return inferred, nil
	}
	type candidate struct {
		app   *App
		score int
	}
	best := candidate{app: inferred}
	for _, id := range ids {
		var candidateApp *App
		if id == inferred.ProjectID {
			candidateApp = inferred
		} else {
			candidateApp, err = NewForProject(id)
			if err != nil {
				continue
			}
		}
		score := hookOwnershipScore(candidateApp, input)
		if score > best.score {
			best = candidate{app: candidateApp, score: score}
		}
	}
	return best.app, nil
}

func hookOwnershipScore(a *App, input HookInput) int {
	st, err := a.Store.Read()
	if err != nil {
		return 0
	}
	if input.SessionID != "" {
		if input.SessionID == st.Sol.CodexSessionID {
			return 10_000
		}
		for _, worker := range st.Workers {
			if input.SessionID == worker.CodexSessionID {
				return 10_000
			}
		}
	}

	best := 0
	for _, task := range state.RunningTasks(&st) {
		if pathContains(task.Workspace, input.CWD) {
			score := 5_000 + pathDepth(task.Workspace)
			if score > best {
				best = score
			}
		}
	}
	if pathContains(st.Sol.CWD, input.CWD) {
		score := 1_000 + pathDepth(st.Sol.CWD)
		if score > best {
			best = score
		}
	}
	return best
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	if clean == "." || clean == string(os.PathSeparator) {
		return 0
	}
	depth := 0
	for current := clean; current != "." && current != string(os.PathSeparator); {
		depth++
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return depth
}
