package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
)

func TestParseGlobalProjectOption(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		project string
		command string
	}{
		{[]string{"--project", "Project1", "status"}, "project1", "status"},
		{[]string{"-p", "project2", "inbox"}, "project2", "inbox"},
		{[]string{"--project=project1-ios", "doctor"}, "project1-ios", "doctor"},
		{[]string{"status"}, "", "status"},
	} {
		project, rest, err := parseGlobalOptions(tc.args)
		if err != nil {
			t.Fatalf("parseGlobalOptions(%v): %v", tc.args, err)
		}
		if project != tc.project || len(rest) == 0 || rest[0] != tc.command {
			t.Fatalf("parseGlobalOptions(%v) = project=%q rest=%v", tc.args, project, rest)
		}
	}
}

func TestGUISnapshotHistorySelectionIsBoundedAndKeepsActiveRecords(t *testing.T) {
	tasks := make([]*state.Task, 0, guiMaxSelectedRecords+20)
	deliveries := make([]*state.Delivery, 0, guiMaxSelectedRecords+20)
	for index := 0; index < guiMaxSelectedRecords+20; index++ {
		status := state.TaskFinished
		deliveryStatus := state.DeliveryDelivered
		if index == guiMaxRecentRecords+5 {
			status = state.TaskRunning
			deliveryStatus = state.DeliveryPending
		}
		created := time.Unix(int64(guiMaxSelectedRecords+20-index), 0)
		tasks = append(tasks, &state.Task{ID: fmt.Sprintf("task-%d", index), Status: status, CreatedAt: created})
		deliveries = append(deliveries, &state.Delivery{ID: fmt.Sprintf("delivery-%d", index), Status: deliveryStatus, CreatedAt: created})
	}
	selectedTasks := selectRecentTasks(tasks)
	selectedDeliveries := selectRecentDeliveries(deliveries)
	if len(selectedTasks) > guiMaxSelectedRecords || len(selectedDeliveries) > guiMaxSelectedRecords {
		t.Fatalf("history selection exceeded cap: tasks=%d deliveries=%d", len(selectedTasks), len(selectedDeliveries))
	}
	if !containsTask(selectedTasks, "task-105") || !containsDelivery(selectedDeliveries, "delivery-105") {
		t.Fatal("active records outside the recent window were dropped")
	}
}

func TestGUIWorkerSessionTemplateSupportsNamedProjectsWithCustomDefaultPattern(t *testing.T) {
	customPattern := `^worker-(alpha|beta)$`
	if got := guiWorkerSessionTemplate("default", customPattern); got != "" {
		t.Fatalf("custom default pattern should not be synthesized, got %q", got)
	}
	if got := guiWorkerSessionTemplate("chapter", customPattern); got != "chapter--worker-%d" {
		t.Fatalf("named project template = %q", got)
	}
}

func TestGUISessionProfileRejectsBindingFromReusedTmuxName(t *testing.T) {
	created := time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC)
	worker := state.Worker{
		Pane:                   "%1",
		CodexSessionObservedAt: created.Add(900 * time.Millisecond),
		TmuxSessionCreatedAt:   created.Add(-time.Minute),
		UpdatedAt:              created.Add(time.Second),
	}
	live := tmux.Pane{ID: "%2", Session: "worker-1", Command: "codex", Active: true}
	if sessionProfileBindingIsCurrent("worker-1", worker.Pane, live, worker.CodexSessionObservedAt, worker.TmuxSessionCreatedAt, map[string]time.Time{"worker-1": created}) {
		t.Fatal("binding from a previous tmux session was accepted after name reuse")
	}
	worker.Pane = "%2"
	worker.TmuxSessionCreatedAt = created
	if !sessionProfileBindingIsCurrent("worker-1", worker.Pane, live, worker.CodexSessionObservedAt, worker.TmuxSessionCreatedAt, map[string]time.Time{"worker-1": created}) {
		t.Fatal("same-second hook binding for the current pane was rejected")
	}
	if sessionBindingIsCurrent("worker-2", created.Add(time.Second), map[string]time.Time{"worker-1": created}) {
		t.Fatal("binding without an authoritative tmux creation time was accepted")
	}
	live.Command = "zsh"
	if sessionProfileBindingIsCurrent("worker-1", worker.Pane, live, worker.CodexSessionObservedAt, worker.TmuxSessionCreatedAt, map[string]time.Time{"worker-1": created}) {
		t.Fatal("old Codex profile was accepted while the live pane was a shell")
	}
	live.Command = "/opt/homebrew/bin/codex-aarch64-apple-darwin"
	if !sessionProfileBindingIsCurrent("worker-1", worker.Pane, live, worker.CodexSessionObservedAt, worker.TmuxSessionCreatedAt, map[string]time.Time{"worker-1": created}) {
		t.Fatal("live Codex pane was rejected")
	}
}

func TestGUIPendingHandoffTextIsNotStarvedByLargeGoal(t *testing.T) {
	directory := t.TempDir()
	goalPath := filepath.Join(directory, "goal.md")
	handoffPath := filepath.Join(directory, "handoff.md")
	if err := os.WriteFile(goalPath, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffPath, []byte("priority handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := guiProjectSnapshot{
		GoalTexts:            map[string]string{},
		GoalTextTruncated:    map[string]bool{},
		HandoffMessages:      map[string]string{},
		HandoffTextTruncated: map[string]bool{},
	}
	tasks := []*state.Task{{ID: "task", ObjectivePath: goalPath}}
	deliveries := []*state.Delivery{{ID: "handoff", MessagePath: handoffPath, Status: state.DeliveryPending}}
	populateProjectText(&project, tasks, deliveries, "", 1024)
	if project.HandoffMessages["handoff"] != "priority handoff" {
		t.Fatalf("pending handoff was starved: %q", project.HandoffMessages["handoff"])
	}
	if len(project.GoalTexts["task"]) == 0 {
		t.Fatal("goal fair-share preview was unexpectedly empty")
	}
	if !project.GoalTextTruncated["task"] {
		t.Fatal("bounded goal preview was not marked truncated")
	}
}

func containsTask(tasks []*state.Task, id string) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

func containsDelivery(deliveries []*state.Delivery, id string) bool {
	for _, delivery := range deliveries {
		if delivery.ID == id {
			return true
		}
	}
	return false
}

func TestParseGlobalProjectOptionRejectsAmbiguousName(t *testing.T) {
	if _, _, err := parseGlobalOptions([]string{"--project", "a--b", "status"}); err == nil {
		t.Fatal("expected invalid project to fail")
	}
}

func TestGUIFileReadersBoundContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefix, truncated, err := readTextPrefix(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "0123" {
		t.Fatalf("prefix = %q", prefix)
	}
	if !truncated {
		t.Fatal("bounded prefix was not marked truncated")
	}
	tail, err := readTextTail(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	if tail != "6789" {
		t.Fatalf("tail = %q", tail)
	}
}

func TestGUISnapshotNormalizesEmptyCollections(t *testing.T) {
	snapshot := guiSnapshot{Projects: []guiProjectSnapshot{{ID: "empty"}}}
	snapshot.normalizeCollections()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		TmuxSessions    []string                     `json:"tmux_sessions"`
		SessionProfiles map[string]guiSessionProfile `json:"session_profiles"`
		Projects        []struct {
			WorkerSessions       []string          `json:"worker_sessions"`
			TaskOrder            []string          `json:"task_order"`
			HandoffOrder         []string          `json:"handoff_order"`
			GoalTexts            map[string]string `json:"goal_texts"`
			GoalTextTruncated    map[string]bool   `json:"goal_text_truncated"`
			HandoffMessages      map[string]string `json:"handoff_messages"`
			HandoffTextTruncated map[string]bool   `json:"handoff_message_truncated"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TmuxSessions == nil || decoded.SessionProfiles == nil || len(decoded.Projects) != 1 {
		t.Fatalf("snapshot collections were not normalized: %s", data)
	}
	if snapshot.SessionAttention == nil {
		t.Fatal("session attention was not normalized")
	}
	project := decoded.Projects[0]
	if project.WorkerSessions == nil || project.TaskOrder == nil || project.HandoffOrder == nil || project.GoalTexts == nil || project.GoalTextTruncated == nil || project.HandoffMessages == nil || project.HandoffTextTruncated == nil {
		t.Fatalf("project collections were not normalized: %s", data)
	}
}

func TestGUISessionProfileAlwaysEncodesBothKeys(t *testing.T) {
	data, err := json.Marshal(guiSessionProfile{Model: "gpt-5.6-luna"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"model":"gpt-5.6-luna","effort":""}` {
		t.Fatalf("partial profile JSON = %s", data)
	}
}

func TestGUISessionSnapshotEncodesEmptySessionsAsArray(t *testing.T) {
	probe := guiSessionSnapshot{}
	if probe.TmuxSessions == nil {
		probe.TmuxSessions = []string{}
	}
	data, err := json.Marshal(probe)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		TmuxSessions []string `json:"tmux_sessions"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TmuxSessions == nil {
		t.Fatalf("session probe encoded null collection: %s", data)
	}
}

func TestPaneShowsActiveTurnUsesInterruptMarker(t *testing.T) {
	if !paneShowsActiveTurn("• Working (28s • esc to interrupt) · 1 background terminal running") {
		t.Fatal("active Codex turn was not detected")
	}
	if paneShowsActiveTurn("Goal stalled (/goal resume)\n› Ask Codex to do anything") {
		t.Fatal("stalled goal without an active turn was reported busy")
	}
	if paneShowsActiveTurn("› Please print the phrase esc to interrupt\n› Ask Codex to do anything") {
		t.Fatal("prompt text containing the marker was reported busy")
	}
	if !paneShowsActiveTurn("Working (2s • esc to cancel)") {
		t.Fatal("active cancel marker was not detected")
	}
}

func TestPaneShowsActiveTurnUsesLatestLifecycleMarker(t *testing.T) {
	idle := strings.Join([]string{
		"• Working (28s • esc to interrupt)",
		"• Received: worker started; now waiting without polling.",
		"— Worked for 1m 03s —",
		"› Ask Codex to do anything",
	}, "\n")
	if paneShowsActiveTurn(idle) {
		t.Fatal("a completed turn was reported busy because an older Working marker remained in scrollback")
	}

	paused := "• Working (2s • esc to interrupt)\n• Goal paused Objective: wait for handoffs"
	if paneShowsActiveTurn(paused) {
		t.Fatal("a paused goal was reported as an active turn")
	}

	restarted := "— Worked for 1m 03s —\n• Working (2s • esc to interrupt)"
	if !paneShowsActiveTurn(restarted) {
		t.Fatal("a newer active turn did not override an older completion marker")
	}
}

func TestPaneShowsEmptyComposerUsesLatestComposerLine(t *testing.T) {
	if !paneShowsEmptyComposer("— Worked for 3s —\n› Ask Codex to do anything") {
		t.Fatal("empty composer was not detected")
	}
	if paneShowsEmptyComposer("› Ask Codex to do anything\n› unsent draft") {
		t.Fatal("older empty composer hid a newer unsent draft")
	}
	if paneShowsEmptyComposer("• Working (2s • esc to interrupt)") {
		t.Fatal("active pane was reported as an empty composer")
	}
}

func TestBrainSetupRequiresIdleEmptyComposer(t *testing.T) {
	if err := validateBrainSetupPane("• Working (2s • esc to interrupt)\n› Ask Codex to do anything"); err == nil {
		t.Fatal("active Brain accepted a setup prompt")
	}
	if err := validateBrainSetupPane("— Worked for 3s —\n› unsent draft"); err == nil {
		t.Fatal("Brain with an unsent draft accepted a setup prompt")
	}
	if err := validateBrainSetupPane("— Worked for 3s —\n› Ask Codex to do anything"); err != nil {
		t.Fatalf("idle Brain with empty composer rejected: %v", err)
	}
}

func TestGUIActivityProbeTracksOnlyConductorSessions(t *testing.T) {
	cfg := config.Default()
	got := conductorSessions([]string{
		"brain", "worker-1", "chapter--brain", "chapter--worker-2", "unrelated", "legacy--assistant-1",
	}, cfg)
	want := []string{"brain", "worker-1", "chapter--brain", "chapter--worker-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked sessions = %v, want %v", got, want)
	}
}

func TestGUIActivityBatchDoesNotProjectIdleWhenTmuxAborts(t *testing.T) {
	fakeTmux := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	activity, attention := detectSessionSignals(fakeTmux, []string{"brain", "worker-1"})
	if len(activity) != 0 || len(attention) != 0 {
		t.Fatalf("failed batch projected signals: activity=%+v attention=%+v", activity, attention)
	}
}

func TestGUIActivityBatchParsesEveryCompletedSession(t *testing.T) {
	fakeTmux := filepath.Join(t.TempDir(), "tmux")
	script := `#!/bin/sh
for argument do
  case "$argument" in
    *__CONDUCTOR_ACTIVITY_*_0_BEGIN__) printf '%s\n' "$argument" '• Working (2s • esc to interrupt)' ;;
    *__CONDUCTOR_ACTIVITY_*_1_BEGIN__) printf '%s\n' "$argument" '› Ask Codex to do anything' ;;
    *__CONDUCTOR_ACTIVITY_*_END__) printf '%s\n' "$argument" ;;
  esac
done
`
	if err := os.WriteFile(fakeTmux, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	activity, attention := detectSessionSignals(fakeTmux, []string{"brain", "worker-1"})
	if !activity["brain"] || activity["worker-1"] {
		t.Fatalf("parsed batch activity = %+v", activity)
	}
	if attention["brain"] || attention["worker-1"] {
		t.Fatalf("unexpected attention signals = %+v", attention)
	}
}

func TestGUISessionProbeDetectsReplaceGoalAttention(t *testing.T) {
	fakeTmux := filepath.Join(t.TempDir(), "tmux")
	script := `#!/bin/sh
for argument do
  case "$argument" in
    *__CONDUCTOR_ACTIVITY_*_0_BEGIN__)
      printf '%s\n' "$argument" 'Replace goal?' '1. Replace current goal  Set the new objective and start it now' '2. Cancel  Keep the current goal'
      ;;
    *__CONDUCTOR_ACTIVITY_*_END__) printf '%s\n' "$argument" ;;
  esac
done
`
	if err := os.WriteFile(fakeTmux, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	activity, attention := detectSessionSignals(fakeTmux, []string{"worker-1"})
	if activity["worker-1"] || !attention["worker-1"] {
		t.Fatalf("replace dialog signals: activity=%+v attention=%+v", activity, attention)
	}
}

func TestGUISessionProbePrefersCurrentWorkingOverHistoricalReplaceDialog(t *testing.T) {
	fakeTmux := filepath.Join(t.TempDir(), "tmux")
	script := `#!/bin/sh
for argument do
  case "$argument" in
    *__CONDUCTOR_ACTIVITY_*_0_BEGIN__)
      printf '%s\n' "$argument" 'Replace goal?' '1. Replace current goal  Set the new objective and start it now' '2. Cancel  Keep the current goal' '• Working (2s • esc to interrupt)'
      ;;
    *__CONDUCTOR_ACTIVITY_*_END__) printf '%s\n' "$argument" ;;
  esac
done
`
	if err := os.WriteFile(fakeTmux, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	activity, attention := detectSessionSignals(fakeTmux, []string{"worker-1"})
	if !activity["worker-1"] || attention["worker-1"] {
		t.Fatalf("historical dialog overrode current activity: activity=%+v attention=%+v", activity, attention)
	}
}

func TestGUIModelCatalogDecodesArrayAndWrappedShapes(t *testing.T) {
	for _, input := range []string{
		`[{"slug":"custom-model","display_name":"Custom","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high","description":"Deep"}]}]`,
		`{"models":[{"slug":"custom-model","supported_reasoning_levels":[]}]}`,
	} {
		models, err := decodeGUIModelCatalog([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 1 || models[0].Slug != "custom-model" {
			t.Fatalf("decoded models = %+v", models)
		}
	}
}

func TestGUIModelCatalogPrefersLiveAndFallsBackToBundled(t *testing.T) {
	live := []guiCodexModel{{Slug: "gpt-daybreak-blue-latest"}}
	bundled := []guiCodexModel{{Slug: "bundled-fallback"}}
	got, err := resolveGUIModelCatalog(
		func() ([]guiCodexModel, error) { return live, nil },
		func() ([]guiCodexModel, error) { return bundled, nil },
	)
	if err != nil || !reflect.DeepEqual(got, live) {
		t.Fatalf("live catalog = %+v, %v", got, err)
	}

	got, err = resolveGUIModelCatalog(
		func() ([]guiCodexModel, error) { return nil, errors.New("offline") },
		func() ([]guiCodexModel, error) { return bundled, nil },
	)
	if err != nil || !reflect.DeepEqual(got, bundled) {
		t.Fatalf("bundled fallback = %+v, %v", got, err)
	}
}

func TestGUIModelCatalogIncludesOnlyModelsCodexMarksVisible(t *testing.T) {
	models := visibleGUIModels([]guiCodexModel{
		{Slug: "gpt-daybreak-blue-latest", Visibility: "list"},
		{Slug: "gpt-reserve", Visibility: "hide"},
		{Slug: "codex-auto-review", Visibility: "hide"},
	})
	if len(models) != 1 || models[0].Slug != "gpt-daybreak-blue-latest" {
		t.Fatalf("visible models = %+v", models)
	}
}

func TestApplyLiveActivityOverridesStaleHookFlagsWithoutMutatingWorkers(t *testing.T) {
	originalWorker := &state.Worker{Session: "worker-1", Busy: true}
	display := state.State{
		Brain:   state.Activity{Session: "brain", Busy: false},
		Workers: map[string]*state.Worker{"worker-1": originalWorker},
	}
	applyLiveActivity(&display, "brain", map[string]bool{"brain": true, "worker-1": false})
	if !display.Brain.Busy || display.Workers["worker-1"].Busy {
		t.Fatalf("live activity was not authoritative: %+v", display)
	}
	if !originalWorker.Busy {
		t.Fatal("live projection mutated persisted worker state")
	}
}
