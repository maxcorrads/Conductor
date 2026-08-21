package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
)

func TestDeleteProjectRemovesOnlyNamedRuntimeDirectory(t *testing.T) {
	base := t.TempDir()
	projectHome := filepath.Join(base, "projects", "demo")
	paths := config.ForProject(config.Paths{Root: base, Home: base, ProjectsDir: filepath.Join(base, "projects")}, "demo")
	if err := os.MkdirAll(projectHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	a := &App{
		ProjectID: "demo",
		BasePaths: config.Paths{ProjectsDir: filepath.Join(base, "projects")},
		Paths:     paths,
		Config:    config.Default(),
		Tmux:      &fakeTmux{panes: map[string]tmux.Pane{}},
	}
	if err := a.DeleteProject(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(projectHome); !os.IsNotExist(err) {
		t.Fatalf("project runtime directory still exists: %v", err)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("workspace was changed: %v", err)
	}
}

func TestDeleteProjectRefusesDefaultAndConnectedSessions(t *testing.T) {
	defaultApp := &App{ProjectID: "default"}
	if err := defaultApp.DeleteProject(); err == nil {
		t.Fatal("default project deletion unexpectedly succeeded")
	}

	base := t.TempDir()
	paths := config.ForProject(config.Paths{Root: base, Home: base, ProjectsDir: filepath.Join(base, "projects")}, "demo")
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{
		ProjectID: "demo",
		BasePaths: config.Paths{ProjectsDir: filepath.Join(base, "projects")},
		Paths:     paths,
		Config:    config.Default(),
		Tmux: &fakeTmux{panes: map[string]tmux.Pane{
			"demo--brain": {Session: "demo--brain"},
		}},
	}
	if err := a.DeleteProject(); err == nil {
		t.Fatal("connected project deletion unexpectedly succeeded")
	}
	if _, err := os.Stat(paths.State); err != nil {
		t.Fatalf("connected project state was changed: %v", err)
	}
}

func TestNewForHookFindsNamedProjectByCodexSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CONDUCTOR_HOME", home)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")

	project1, err := NewForProject("project1")
	if err != nil {
		t.Fatal(err)
	}
	if err := project1.Init(); err != nil {
		t.Fatal(err)
	}
	project2, err := NewForProject("project2")
	if err != nil {
		t.Fatal(err)
	}
	if err := project2.Init(); err != nil {
		t.Fatal(err)
	}
	if err := project2.Store.Update(func(st *state.State) error {
		st.Brain.CodexSessionID = "project2-brain-session"
		st.Brain.CWD = "/repo/project2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got, err := NewForHook(HookInput{SessionID: "project2-brain-session", CWD: "/repo/project2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "project2" || got.BrainSession != "project2--brain" {
		t.Fatalf("hook routed to wrong project: project=%s brain=%s", got.ProjectID, got.BrainSession)
	}
}
