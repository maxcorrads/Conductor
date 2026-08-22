package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestLatestGoalEventForThreadExposesMismatchedObjective(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := `{"timestamp":"2026-08-20T11:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"thr-1","goal":{"objective":"unexpected goal","status":"active"}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	event, found, err := LatestGoalEventForThread(path, "thr-1", time.Time{}, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !found || event.Objective != "unexpected goal" || event.Status != "active" {
		t.Fatalf("got found=%v event=%+v", found, event)
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

func TestGoalStatusFromSQLiteCheckedReportsUnavailableLookup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	event, found, checked, err := GoalStatusFromSQLiteChecked("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if found || checked || event.Status != "" {
		t.Fatalf("unavailable sqlite lookup reported evidence: event=%+v found=%v checked=%v", event, found, checked)
	}
}

func TestSessionProfilesFromSQLiteCheckedReadsCurrentModelAndEffort(t *testing.T) {
	bin := t.TempDir()
	sqlite := filepath.Join(bin, "sqlite3")
	script := "#!/bin/sh\ncase \" $* \" in *\" -readonly -noheader \"*) ;; *) exit 2 ;; esac\nprintf 'thread-1\\tgpt-5.6-luna\\tmax\\nthread-2\\tgpt-5.6-sol\\tultra\\nunrequested\\tgpt-5.6-terra\\thigh\\n'\n"
	if err := os.WriteFile(sqlite, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "state_5.sqlite"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", codexHome)

	profiles, checked, err := SessionProfilesFromSQLiteChecked([]string{"thread-1", "thread-2", "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("existing Codex database was not checked")
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
	if got := profiles["thread-1"]; got.Model != "gpt-5.6-luna" || got.Effort != "max" {
		t.Fatalf("thread-1 profile = %+v", got)
	}
	if got := profiles["thread-2"]; got.Model != "gpt-5.6-sol" || got.Effort != "ultra" {
		t.Fatalf("thread-2 profile = %+v", got)
	}
	if _, leaked := profiles["unrequested"]; leaked {
		t.Fatal("unrequested thread profile was accepted")
	}
}

func TestSessionProfilesEscapeRequestedThreadIDs(t *testing.T) {
	bin := t.TempDir()
	sqlite := filepath.Join(bin, "sqlite3")
	script := "#!/bin/sh\nprintf '%s' \"$4\" > \"$QUERY_CAPTURE\"\nprintf \"x' OR 1=1 --\\tgpt-5.6-luna\\tmax\\nunrequested\\tstale-model\\tlow\\n\"\n"
	if err := os.WriteFile(sqlite, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "state_5.sqlite"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "query.txt")
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("QUERY_CAPTURE", capture)

	malicious := "x' OR 1=1 --"
	profiles, checked, err := SessionProfilesFromSQLiteChecked([]string{malicious})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("existing Codex database was not checked")
	}
	query, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(query), "WHERE id IN ('x'' OR 1=1 --');") {
		t.Fatalf("thread id was not SQL-escaped: %s", query)
	}
	if len(profiles) != 1 || profiles[malicious].Model != "gpt-5.6-luna" {
		t.Fatalf("requested profile was not isolated: %+v", profiles)
	}
	if _, leaked := profiles["unrequested"]; leaked {
		t.Fatal("unrequested thread profile was accepted")
	}
}

func TestSessionProfilesFromSQLiteCheckedFailsSoftWithoutSQLite(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	profiles, checked, err := SessionProfilesFromSQLiteChecked([]string{"thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if checked || len(profiles) != 0 {
		t.Fatalf("unavailable lookup reported metadata: checked=%v profiles=%+v", checked, profiles)
	}
}

func TestSessionProfilesDoNotFallBackAfterNewestDatabaseError(t *testing.T) {
	bin := t.TempDir()
	sqlite := filepath.Join(bin, "sqlite3")
	script := `#!/bin/sh
case "$3" in
  *state_5.sqlite) printf 'newest database unavailable' >&2; exit 1 ;;
  *state_4.sqlite) printf 'thread-1\tstale-model\tlow\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(sqlite, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	for _, name := range []string{"state_5.sqlite", "state_4.sqlite"} {
		if err := os.WriteFile(filepath.Join(codexHome, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", codexHome)
	profiles, checked, err := SessionProfilesFromSQLiteChecked([]string{"thread-1"})
	if err == nil || !checked {
		t.Fatalf("newest database error was not authoritative: checked=%v err=%v", checked, err)
	}
	if len(profiles) != 0 {
		t.Fatalf("stale fallback profile was returned: %+v", profiles)
	}
}

func TestSessionProfilesTreatFutureStateDatabaseAsAuthoritative(t *testing.T) {
	bin := t.TempDir()
	sqlite := filepath.Join(bin, "sqlite3")
	script := `#!/bin/sh
case "$3" in
  *state_6.sqlite) printf 'unsupported future schema' >&2; exit 1 ;;
  *state_5.sqlite) printf 'thread-1\tstale-model\tlow\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(sqlite, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	for _, name := range []string{"state_6.sqlite", "state_5.sqlite"} {
		if err := os.WriteFile(filepath.Join(codexHome, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", codexHome)
	profiles, checked, err := SessionProfilesFromSQLiteChecked([]string{"thread-1"})
	if err == nil || !checked {
		t.Fatalf("future database was not authoritative: checked=%v err=%v", checked, err)
	}
	if len(profiles) != 0 {
		t.Fatalf("older database profile leaked through: %+v", profiles)
	}
}
