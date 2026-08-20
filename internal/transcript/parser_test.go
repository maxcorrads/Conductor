package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestGoalEventFindsTerminalStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	objective := "Implement the cache fix."
	content := fmt.Sprintf(`{"timestamp":"2026-08-20T10:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":%q,"status":"active"}}}
{"timestamp":"2026-08-20T10:05:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":%q,"status":"complete"}}}
{"timestamp":"2026-08-20T10:05:01Z","type":"response_item","payload":{"type":"message","role":"assistant"}}
`, objective, objective)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	event, found, err := LatestGoalEvent(path, "thr-1", objective, time.Date(2026, 8, 20, 9, 59, 0, 0, time.UTC), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !found || event.Status != "complete" {
		t.Fatalf("got found=%v event=%+v", found, event)
	}
}

func TestLatestGoalEventReturnsLatestActiveInsteadOfStaleTerminal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-08-20T10:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":"same","status":"complete"}}}
{"timestamp":"2026-08-20T11:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":"same","status":"active"}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	event, found, err := LatestGoalEvent(path, "thr-1", "same", time.Time{}, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !found || event.Status != "active" {
		t.Fatalf("got found=%v event=%+v", found, event)
	}
}

func TestLatestGoalEventFallsBackToObjectiveWhenThreadIDIsMismatched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-08-20T11:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"wrong-thread","goal":{"objective":"target objective","status":"blocked"}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	event, found, err := LatestGoalEvent(path, "expected-thread", "target objective", time.Time{}, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !found || event.Status != "blocked" {
		t.Fatalf("got found=%v event=%+v", found, event)
	}
}

func TestLatestGoalEventRejectsMismatchedObjectiveInSameThread(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-08-20T11:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":"old objective","status":"complete"}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, found, err := LatestGoalEvent(path, "thr-1", "new objective", time.Time{}, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("stale goal with a different objective was accepted")
	}
}

func TestLatestGoalEventReadsTailOfLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20_000; i++ {
		fmt.Fprintf(file, "{\"type\":\"noise\",\"n\":%d}\n", i)
	}
	fmt.Fprintln(file, `{"timestamp":"2026-08-20T11:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr","goal":{"objective":"goal","status":"complete"}}}`)
	file.Close()
	event, found, err := LatestGoalEvent(path, "thr", "goal", time.Time{}, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !found || event.Status != "complete" {
		t.Fatalf("got found=%v event=%+v", found, event)
	}
}
