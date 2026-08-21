package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxcorrads/conductor/internal/state"
)

func TestMissedGoalLifecycleProducesImplicitHandoffAfterOneShotReconciliation(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a, fake, _ := testApp(t)
	a.Config.GoalReconcileDelayMS = 0
	task, err := a.Delegate("luna-1", "Investigate and return a handoff")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	transcriptPath := filepath.Join(a.Paths.Home, "normal-turn.jsonl")
	if err := os.WriteFile(transcriptPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	message := "## Handoff\n\nThe work is complete, but Codex handled the assignment as a normal prompt.  "
	fake.setCurrent("luna-1")
	if err := a.HandleHook("stop", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		TranscriptPath: &transcriptPath, LastAssistantMessage: &message,
	}); err != nil {
		t.Fatal(err)
	}

	prompts := fake.prompts()
	if len(prompts) != 2 {
		t.Fatalf("expected goal and implicit handoff, got %+v", prompts)
	}
	prefix := "[CONDUCTOR HANDOFF | luna-1 | implicit]\nworkspace: " + task.Workspace + "\n\n"
	if !strings.HasPrefix(prompts[1].text, prefix) {
		t.Fatalf("implicit envelope missing: %q", prompts[1].text)
	}
	if prompts[1].text[len(prefix):] != message {
		t.Fatalf("implicit handoff was rewritten\nwant: %q\n got: %q", message, prompts[1].text[len(prefix):])
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Tasks[task.ID].Status != state.TaskFinished || st.Tasks[task.ID].TerminalGoalStatus != "implicit" {
		t.Fatalf("task was not finished implicitly: %+v", st.Tasks[task.ID])
	}
}

func TestImplicitReconciliationRequiresReadableGoalSource(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a, fake, _ := testApp(t)
	a.Config.GoalReconcileDelayMS = 0
	task, err := a.Delegate("luna-1", "Task without local goal artifacts")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("luna-1")
	message := "A final response exists, but there is no transcript or goal database to verify the lifecycle."
	if err := a.HandleHook("stop", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		LastAssistantMessage: &message,
	}); err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	current := st.Tasks[task.ID]
	if current.Status != state.TaskRunning || current.ReconcileToken != "" {
		t.Fatalf("unverifiable goal absence was inferred complete: %+v", current)
	}
	if !strings.Contains(current.LastError, "no readable Codex goal source") {
		t.Fatalf("missing recovery diagnostic: %+v", current)
	}
	if len(fake.prompts()) != 1 {
		t.Fatal("unverifiable reconciliation unexpectedly woke Sol")
	}
}

func TestObservedActiveGoalIsNeverInferredComplete(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a, fake, _ := testApp(t)
	a.Config.GoalReconcileDelayMS = 0
	task, err := a.Delegate("luna-1", "Long-running goal")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("luna-1")
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"active"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"active"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	message := "Intermediate checkpoint"
	if err := a.HandleHook("stop", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		LastAssistantMessage: &message,
	}); err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	current := st.Tasks[task.ID]
	if current.ObservedGoalStatus != "active" || current.ReconcileToken != "" || current.Status != state.TaskRunning {
		t.Fatalf("active goal became eligible for implicit completion: %+v", current)
	}
	if len(fake.prompts()) != 1 {
		t.Fatal("active goal unexpectedly woke Sol")
	}
}

func TestLaterWorkerTurnCancelsImplicitReconciliation(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a, fake, _ := testApp(t)
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(a.Paths.Home, "cancelled.jsonl")
	if err := os.WriteFile(transcriptPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	messagePath := filepath.Join(a.Paths.CacheDir, task.ID+"-last-assistant.md")
	if err := os.WriteFile(messagePath, []byte("First normal-turn response"), 0o600); err != nil {
		t.Fatal(err)
	}
	token := "rec-cancelled"
	if err := a.Store.Update(func(st *state.State) error {
		current := st.Tasks[task.ID]
		current.ReconcileToken = token
		current.TranscriptPath = transcriptPath
		current.LastAssistantPath = messagePath
		current.CodexSessionID = "worker-thread"
		st.Workers[task.WorkerSession].Busy = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("luna-1")
	if err := a.HandleHook("user-prompt-submit", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "turn-two",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.ReconcileTask(task.ID, token, 0); err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Tasks[task.ID].Status != state.TaskRunning || st.Tasks[task.ID].ReconcileToken != "" {
		t.Fatalf("cancelled reconciliation changed the task: %+v", st.Tasks[task.ID])
	}
}

func TestReconciliationPrefersLateTerminalGoalOverImplicitStatus(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a, fake, clock := testApp(t)
	task, err := a.Delegate("luna-1", "Task with delayed goal persistence")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(a.Paths.Home, "late-goal.jsonl")
	messagePath := filepath.Join(a.Paths.CacheDir, task.ID+"-last-assistant.md")
	message := "Final response written before the goal event was flushed."
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	token := "rec-late-goal"
	if err := a.Store.Update(func(st *state.State) error {
		current := st.Tasks[task.ID]
		current.ReconcileToken = token
		current.TranscriptPath = transcriptPath
		current.LastAssistantPath = messagePath
		current.CodexSessionID = "worker-thread"
		st.Workers[task.WorkerSession].Busy = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("{\"timestamp\":%q,\"payload\":{\"type\":\"thread_goal_updated\",\"threadId\":\"worker-thread\",\"goal\":{\"objective\":%q,\"status\":\"complete\"}}}\n", clock.Add(time.Second).Format(time.RFC3339), task.SentGoalObjective)
	if err := os.WriteFile(transcriptPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ReconcileTask(task.ID, token, 0); err != nil {
		t.Fatal(err)
	}
	prompts := fake.prompts()
	if len(prompts) != 2 || !strings.Contains(prompts[1].text, "[CONDUCTOR HANDOFF | luna-1 | complete]") {
		t.Fatalf("late terminal goal was not preferred: %+v", prompts)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Tasks[task.ID].TerminalGoalStatus != "complete" || st.Tasks[task.ID].ObservedGoalStatus != "complete" {
		t.Fatalf("late goal state was not recorded: %+v", st.Tasks[task.ID])
	}
}
