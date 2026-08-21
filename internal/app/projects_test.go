package app

import (
	"testing"

	"github.com/maxcorrads/conductor/internal/state"
)

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
