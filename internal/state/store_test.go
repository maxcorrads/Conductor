package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxcorrads/conductor/internal/config"
)

func testPaths(dir string) config.Paths {
	return config.Paths{
		Home: dir, Config: filepath.Join(dir, "config.json"), State: filepath.Join(dir, "state.json"),
		Lock: filepath.Join(dir, "state.lock"), TasksDir: filepath.Join(dir, "tasks"),
		HandoffsDir: filepath.Join(dir, "handoffs"), CacheDir: filepath.Join(dir, "cache"),
		LogsDir: filepath.Join(dir, "logs"), Log: filepath.Join(dir, "logs", "conductor.log"),
	}
}

func TestStoreInitializesAndPersists(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()), "sol")
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(st *State) error {
		st.Sol.Busy = true
		st.Tasks["one"] = &Task{ID: "one", WorkerSession: "luna-1", Status: TaskRunning}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Sol.Busy || st.Tasks["one"] == nil {
		t.Fatalf("unexpected state: %+v", st)
	}
}

func TestStoreRecoversStaleSendingDelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := NewStore(testPaths(t.TempDir()), "sol")
	store.Now = func() time.Time { return now }
	if err := store.Update(func(st *State) error {
		st.Deliveries["d"] = &Delivery{ID: "d", Status: DeliverySending, ReservedAt: now.Add(-3 * time.Minute)}
		st.Sol.Busy = true
		st.Sol.ReservedDelivery = "d"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Recovery happens at the beginning of the next locked update.
	if err := store.Update(func(st *State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	st, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Deliveries["d"].Status != DeliveryPending || st.Sol.Busy || st.Sol.ReservedDelivery != "" {
		t.Fatalf("stale reservation not recovered: %+v %+v", st.Deliveries["d"], st.Sol)
	}
}

func TestV01StateWithoutProjectIDLoadsAsDefault(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		Home: dir, Config: filepath.Join(dir, "config.json"), State: filepath.Join(dir, "state.json"), Lock: filepath.Join(dir, "state.lock"),
		TasksDir: filepath.Join(dir, "tasks"), HandoffsDir: filepath.Join(dir, "handoffs"), CacheDir: filepath.Join(dir, "cache"), LogsDir: filepath.Join(dir, "logs"), Log: filepath.Join(dir, "logs", "conductor.log"),
	}
	if err := config.EnsureDirectories(paths); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"sol":{"session":"sol","busy":false},"workers":{},"tasks":{},"deliveries":{}}`
	if err := os.WriteFile(paths.State, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(paths, "sol").Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.ProjectID != "default" {
		t.Fatalf("legacy state assigned to %q, want default", st.ProjectID)
	}
}

func TestStoreRejectsStateFromAnotherProject(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		Home: dir, Config: filepath.Join(dir, "config.json"), State: filepath.Join(dir, "state.json"), Lock: filepath.Join(dir, "state.lock"),
		TasksDir: filepath.Join(dir, "tasks"), HandoffsDir: filepath.Join(dir, "handoffs"), CacheDir: filepath.Join(dir, "cache"), LogsDir: filepath.Join(dir, "logs"), Log: filepath.Join(dir, "logs", "conductor.log"),
	}
	if err := config.EnsureDirectories(paths); err != nil {
		t.Fatal(err)
	}
	foreign := `{"version":1,"project_id":"project1","sol":{"session":"project1--sol","busy":false},"workers":{},"tasks":{},"deliveries":{}}`
	if err := os.WriteFile(paths.State, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectStore(paths, "project2", "project2--sol").Read(); err == nil {
		t.Fatal("expected cross-project state to be rejected")
	}
}
