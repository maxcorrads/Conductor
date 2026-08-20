package config

import (
	"path/filepath"
	"testing"
)

func TestForProjectKeepsDefaultLayoutAndIsolatesNamedProjects(t *testing.T) {
	root := t.TempDir()
	base := Paths{
		Root: root, Home: root, ProjectsDir: filepath.Join(root, "projects"),
		Config: filepath.Join(root, "config.json"), State: filepath.Join(root, "state.json"), Lock: filepath.Join(root, "state.lock"),
		TasksDir: filepath.Join(root, "tasks"), HandoffsDir: filepath.Join(root, "handoffs"), CacheDir: filepath.Join(root, "cache"), LogsDir: filepath.Join(root, "logs"), Log: filepath.Join(root, "logs", "conductor.log"),
	}
	if got := ForProject(base, "default"); got.State != base.State || got.Home != base.Home {
		t.Fatalf("default project layout changed: %+v", got)
	}
	project1 := ForProject(base, "project1")
	project2 := ForProject(base, "project2")
	if project1.Config != base.Config || project2.Config != base.Config {
		t.Fatal("project configuration should remain global")
	}
	if project1.State == project2.State || project1.Home == project2.Home {
		t.Fatalf("named projects are not isolated: project1=%+v project2=%+v", project1, project2)
	}
	want := filepath.Join(root, "projects", "project1", "state.json")
	if project1.State != want {
		t.Fatalf("unexpected project state path: want %s, got %s", want, project1.State)
	}
}
