package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/project"
	"github.com/maxcorrads/conductor/internal/state"
)

// DeleteProject removes only a named project's private Conductor runtime
// directory. It never removes workspaces or tmux sessions, and refuses to run
// while any session for the project is still connected.
func (a *App) DeleteProject() error {
	if a.ProjectID == project.DefaultID {
		return errors.New("the default project cannot be deleted")
	}
	if err := validateProjectDeletePath(a.BasePaths.ProjectsDir, a.Paths.Home); err != nil {
		return err
	}
	if _, err := os.Stat(a.Paths.State); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("project %q is not initialized", a.ProjectID)
	} else if err != nil {
		return fmt.Errorf("inspect project %q: %w", a.ProjectID, err)
	}

	sessions, err := a.Tmux.ListSessions()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		parsed, ok := project.ParseSession(session, a.Config.BrainSession, a.Config.WorkerSessionPattern)
		if ok && parsed.ProjectID == a.ProjectID {
			return fmt.Errorf("project %q still has connected tmux sessions; close its Brain and Workers before deleting it", a.ProjectID)
		}
	}

	if err := os.RemoveAll(a.Paths.Home); err != nil {
		return fmt.Errorf("delete project %q: %w", a.ProjectID, err)
	}
	return nil
}

func validateProjectDeletePath(projectsDir, projectHome string) error {
	for _, path := range []string{projectsDir, projectHome} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect project delete path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to delete through symlinked project path %q", path)
		}
	}

	resolvedRoot, err := filepath.EvalSymlinks(projectsDir)
	if err != nil {
		return fmt.Errorf("resolve projects directory %q: %w", projectsDir, err)
	}
	resolvedHome, err := filepath.EvalSymlinks(projectHome)
	if err != nil {
		return fmt.Errorf("resolve project directory %q: %w", projectHome, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedHome)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || strings.Contains(relative, string(os.PathSeparator)) {
		return fmt.Errorf("refusing to delete unsafe project path %q", projectHome)
	}
	return nil
}

// DiscoverProjectIDs returns every project with local state, plus the default
// project. Named projects are represented by
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
		if parsed, ok := project.ParseSession(session, inferred.Config.BrainSession, inferred.Config.WorkerSessionPattern); ok {
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
		if input.SessionID == st.Brain.CodexSessionID {
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
	if pathContains(st.Brain.CWD, input.CWD) {
		score := 1_000 + pathDepth(st.Brain.CWD)
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
