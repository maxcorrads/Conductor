package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
)

type fakeTmux struct {
	mu      sync.Mutex
	current string
	panes   map[string]tmux.Pane
	sent    []sentPrompt
	sendErr error
}

type sentPrompt struct {
	target string
	text   string
}

type blockingPromptTmux struct {
	*fakeTmux
	started chan struct{}
	release chan struct{}
}

func (f *blockingPromptTmux) SendPrompt(target, prompt string) error {
	close(f.started)
	<-f.release
	return f.fakeTmux.SendPrompt(target, prompt)
}

func TestDefaultWorkerMatcherNeverClaimsNamedProjectSessions(t *testing.T) {
	a := &App{
		ProjectID: "default",
		workerRE:  regexp.MustCompile(`^foo--worker-[1-9][0-9]*$`),
	}
	if a.IsWorkerSession("foo--worker-1") {
		t.Fatal("default project claimed a named-project worker")
	}
}

func TestManualFinishRejectsAReplacementTask(t *testing.T) {
	a, _, _ := testApp(t)
	first, err := a.Delegate("worker-1", "First task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.finishTask(first.ID, "first handoff", "complete", false, nil); err != nil {
		t.Fatal(err)
	}
	second, err := a.Delegate("worker-1", "Replacement task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.FinishWorkerTask("worker-1", first.ID, "stale handoff", "complete"); err == nil {
		t.Fatal("stale confirmation finished the replacement task")
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Tasks[second.ID].Status != state.TaskRunning {
		t.Fatalf("replacement task status = %s", st.Tasks[second.ID].Status)
	}
}

func (f *fakeTmux) CurrentSession() (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pane := f.panes[f.current]
	if f.current == "" {
		return "", "", fmt.Errorf("not in tmux")
	}
	return f.current, pane.ID, nil
}
func (f *fakeTmux) ResolvePane(session string) (tmux.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pane, ok := f.panes[session]
	if !ok {
		return tmux.Pane{}, fmt.Errorf("missing session %s", session)
	}
	return pane, nil
}
func (f *fakeTmux) SessionExists(session string) bool { _, ok := f.panes[session]; return ok }
func (f *fakeTmux) ListSessions() ([]string, error) {
	var out []string
	for name := range f.panes {
		out = append(out, name)
	}
	return out, nil
}
func (f *fakeTmux) SendPrompt(target, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sentPrompt{target: target, text: prompt})
	return nil
}

func (f *fakeTmux) SendGoal(target, objective string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sentPrompt{target: target, text: "/goal " + objective})
	return nil
}
func (f *fakeTmux) Version() (string, error)  { return "tmux fake", nil }
func (f *fakeTmux) setCurrent(session string) { f.mu.Lock(); f.current = session; f.mu.Unlock() }
func (f *fakeTmux) prompts() []sentPrompt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentPrompt(nil), f.sent...)
}

func testApp(t *testing.T) (*App, *fakeTmux, time.Time) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{
		Home: dir, Config: filepath.Join(dir, "config.json"), State: filepath.Join(dir, "state.json"), Lock: filepath.Join(dir, "state.lock"),
		TasksDir: filepath.Join(dir, "tasks"), HandoffsDir: filepath.Join(dir, "handoffs"), CacheDir: filepath.Join(dir, "cache"), LogsDir: filepath.Join(dir, "logs"), Log: filepath.Join(dir, "logs", "conductor.log"),
	}
	cfg := config.Default()
	cfg.DeliveryDelayMS = 0
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeTmux{current: "brain", panes: map[string]tmux.Pane{
		"brain":    {ID: "%1", Session: "brain", Path: "/repo/main", Command: "codex", Active: true},
		"worker-1": {ID: "%2", Session: "worker-1", Path: "/repo/worktrees/worker-1", Command: "codex", Active: true},
		"worker-2": {ID: "%3", Session: "worker-2", Path: "/repo/worktrees/worker-2", Command: "codex", Active: true},
	}}
	store := state.NewStore(paths, cfg.BrainSession)
	store.Now = func() time.Time { return clock }
	a := &App{ProjectID: "default", Paths: paths, Config: cfg, BrainSession: cfg.BrainSession, Store: store, Tmux: fake, Executable: "/tmp/conductor", Now: func() time.Time { return clock }, workerRE: regexp.MustCompile(cfg.WorkerSessionPattern)}
	if err := config.EnsureDirectories(paths); err != nil {
		t.Fatal(err)
	}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	return a, fake, clock
}

func TestSendBrainSetupSerializesAgainstHandoffReservation(t *testing.T) {
	a, fake, now := testApp(t)
	blocking := &blockingPromptTmux{fakeTmux: fake, started: make(chan struct{}), release: make(chan struct{})}
	a.Tmux = blocking
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["handoff-1"] = &state.Delivery{ID: "handoff-1", Status: state.DeliveryPending, CreatedAt: now}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	setupDone := make(chan error, 1)
	go func() {
		setupDone <- a.SendBrainSetup("Coordinate this project.", func(pane tmux.Pane) error { return nil })
	}()
	<-blocking.started

	reservationDone := make(chan error, 1)
	go func() {
		reservationDone <- a.Store.Update(func(st *state.State) error {
			reserveOldestDelivery(st, now.Add(time.Minute))
			return nil
		})
	}()
	select {
	case err := <-reservationDone:
		t.Fatalf("handoff reservation entered while setup paste held the state lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(blocking.release)
	if err := <-setupDone; err != nil {
		t.Fatal(err)
	}
	if err := <-reservationDone; err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Brain.Busy || st.Brain.ReservedDelivery != "" || st.Deliveries["handoff-1"].Status != state.DeliveryPending {
		t.Fatalf("handoff raced setup dispatch: brain=%+v delivery=%+v", st.Brain, st.Deliveries["handoff-1"])
	}
}

func TestSendBrainSetupRejectsReservedHandoffBeforeValidation(t *testing.T) {
	a, fake, now := testApp(t)
	if err := a.Store.Update(func(st *state.State) error {
		st.Brain.ReservedDelivery = "handoff-1"
		st.Brain.Busy = true
		st.Deliveries["handoff-1"] = &state.Delivery{ID: "handoff-1", Status: state.DeliverySending, CreatedAt: now}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	validated := false
	err := a.SendBrainSetup("Coordinate this project.", func(pane tmux.Pane) error {
		validated = true
		return nil
	})
	if err == nil || validated || len(fake.prompts()) != 0 {
		t.Fatalf("reserved handoff did not block setup: err=%v validated=%v prompts=%+v", err, validated, fake.prompts())
	}
}

func TestSendBrainSetupRejectsBusyBrainWithoutTurnID(t *testing.T) {
	a, fake, _ := testApp(t)
	if err := a.Store.Update(func(st *state.State) error {
		st.Brain.Busy = true
		st.Brain.TurnID = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	validated := false
	err := a.SendBrainSetup("Second setup prompt.", func(pane tmux.Pane) error {
		validated = true
		return nil
	})
	if err == nil || validated || len(fake.prompts()) != 0 {
		t.Fatalf("busy Brain accepted a second setup: err=%v validated=%v prompts=%+v", err, validated, fake.prompts())
	}
}

func TestDelegateSendsExactInlineObjective(t *testing.T) {
	a, fake, _ := testApp(t)
	objective := "  Investigate without edits.\n\nReturn any handoff structure Brain requested.  "
	task, err := a.Delegate("worker-1", objective)
	if err != nil {
		t.Fatal(err)
	}
	prompts := fake.prompts()
	if len(prompts) != 1 {
		t.Fatalf("expected one goal, got %+v", prompts)
	}
	if prompts[0].text != "/goal "+objective {
		t.Fatalf("inline objective was changed\nwant: %q\n got: %q", "/goal "+objective, prompts[0].text)
	}
	stored, err := os.ReadFile(task.ObjectivePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != objective {
		t.Fatalf("stored objective was changed: %q", stored)
	}
}

func TestLongGoalUsesPrivateFileReference(t *testing.T) {
	a, fake, _ := testApp(t)
	a.Config.InlineGoalMaxChars = 256
	objective := strings.Repeat("Detailed evidence and constraints.\n", 20) + "Final requirement."
	task, err := a.Delegate("worker-1", objective)
	if err != nil {
		t.Fatal(err)
	}
	prompts := fake.prompts()
	if len(prompts) != 1 {
		t.Fatalf("expected one goal, got %+v", prompts)
	}
	expected := "/goal Read and execute the complete goal in `" + task.ObjectivePath + "`."
	if prompts[0].text != expected {
		t.Fatalf("unexpected long-goal reference\nwant: %q\n got: %q", expected, prompts[0].text)
	}
	stored, err := os.ReadFile(task.ObjectivePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != objective {
		t.Fatalf("long objective was not preserved: %q", stored)
	}
}

func TestMultipleWorkersCanRunInParallel(t *testing.T) {
	a, fake, _ := testApp(t)
	first, err := a.Delegate("worker-1", "Investigate path A")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Delegate("worker-2", "Investigate path B")
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkerSession == second.WorkerSession || first.ID == second.ID {
		t.Fatalf("parallel tasks were not independent: first=%+v second=%+v", first, second)
	}
	prompts := fake.prompts()
	if len(prompts) != 2 || prompts[0].target != "%2" || prompts[1].target != "%3" {
		t.Fatalf("goals were not routed to independent panes: %+v", prompts)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveTaskForWorker(&st, "worker-1") == nil || state.ActiveTaskForWorker(&st, "worker-2") == nil {
		t.Fatalf("parallel active tasks missing: %+v", st.Tasks)
	}
}

func TestVerbatimWorkerHandoffIsPastedIntoBrain(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("worker-1", "Investigate the race. Return any handoff structure you consider useful.")
	if err != nil {
		t.Fatal(err)
	}
	if prompts := fake.prompts(); len(prompts) != 1 || !strings.HasPrefix(prompts[0].text, "/goal ") {
		t.Fatalf("goal was not sent: %+v", prompts)
	}

	// Brain finishes its delegation turn and becomes truly idle.
	fake.setCurrent("brain")
	if err := a.HandleHook("stop", HookInput{SessionID: "brain-thread", CWD: "/repo/main", TurnID: "brain-turn"}); err != nil {
		t.Fatal(err)
	}

	fake.setCurrent("worker-1")
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"complete"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	handoff := "## Handoff\n\n- Root cause: race in SessionStore.\n- No commit was requested.\n\nExact trailing note.  "
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace, LastAssistantMessage: &handoff}); err != nil {
		t.Fatal(err)
	}

	prompts := fake.prompts()
	if len(prompts) != 2 {
		t.Fatalf("expected goal + handoff, got %+v", prompts)
	}
	got := prompts[1].text
	prefix := "[CONDUCTOR HANDOFF | worker-1 | complete]\nworkspace: " + task.Workspace + "\n\n"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("missing envelope: %q", got)
	}
	if got[len(prefix):] != handoff {
		t.Fatalf("handoff was rewritten\nwant: %q\n got: %q", handoff, got[len(prefix):])
	}
}

func TestActiveGoalDoesNotWakeBrain(t *testing.T) {
	a, fake, clock := testApp(t)
	task, err := a.Delegate("worker-1", "Long task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("brain")
	if err := a.HandleHook("stop", HookInput{SessionID: "brain-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(a.Paths.Home, "active.jsonl")
	line := fmt.Sprintf("{\"timestamp\":%q,\"payload\":{\"type\":\"thread_goal_updated\",\"threadId\":\"worker-thread\",\"goal\":{\"objective\":%q,\"status\":\"active\"}}}\n", clock.Add(time.Minute).Format(time.RFC3339), task.SentGoalObjective)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := "Intermediate checkpoint"
	fake.setCurrent("worker-1")
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace, TranscriptPath: &path, LastAssistantMessage: &msg}); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.prompts()); got != 1 {
		t.Fatalf("active goal should not relay; prompt count=%d", got)
	}
	st, _ := a.Store.Read()
	if st.Tasks[task.ID].Status != state.TaskRunning {
		t.Fatalf("task stopped early: %+v", st.Tasks[task.ID])
	}
}

func TestBusyBrainQueuesThenFlushesOneHandoff(t *testing.T) {
	a, fake, clock := testApp(t)
	task, err := a.Delegate("worker-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Paths.Home, "complete.jsonl")
	line := fmt.Sprintf("{\"timestamp\":%q,\"payload\":{\"type\":\"thread_goal_updated\",\"threadId\":\"worker-thread\",\"goal\":{\"objective\":%q,\"status\":\"blocked\"}}}\n", clock.Add(time.Minute).Format(time.RFC3339), task.SentGoalObjective)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := "Blocked because a product decision is required. Options: A or B."
	fake.setCurrent("worker-1")
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace, TranscriptPath: &path, LastAssistantMessage: &msg}); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts()) != 1 {
		t.Fatal("busy Brain should not be interrupted")
	}
	st, _ := a.Store.Read()
	if len(state.PendingDeliveries(&st)) != 1 {
		t.Fatalf("handoff not queued: %+v", st.Deliveries)
	}
	if err := a.MarkBrainIdle(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Flush(false); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts()) != 2 {
		t.Fatal("queued handoff was not flushed")
	}
}

func TestGoalStatusCapturedFromPostToolUse(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("worker-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("worker-1")
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "turn-123",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"blocked"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"blocked"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := st.Tasks[task.ID]
	if got.PendingGoalStatus != "blocked" || got.PendingGoalTurnID != "turn-123" {
		t.Fatalf("terminal status was not captured: %+v", got)
	}
}

func TestTerminalGoalFromDifferentTurnIsIgnored(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("worker-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("brain")
	if err := a.HandleHook("stop", HookInput{SessionID: "brain-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	fake.setCurrent("worker-1")
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "terminal-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"complete"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	unrelated := "A later, unrelated response"
	if err := a.HandleHook("stop", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "different-turn",
		LastAssistantMessage: &unrelated,
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(fake.prompts()); got != 1 {
		t.Fatalf("mismatched Stop must not relay a stale terminal result; prompt count=%d", got)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	got := st.Tasks[task.ID]
	if got.Status != state.TaskRunning {
		t.Fatalf("task finished from a mismatched turn: %+v", got)
	}
	if got.PendingGoalStatus != "" || got.PendingGoalTurnID != "" || !got.PendingGoalAt.IsZero() {
		t.Fatalf("stale terminal marker was not cleared: %+v", got)
	}
}

func TestFailedUpdateGoalResponseIsNotTreatedAsTerminal(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("worker-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("worker-1")
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "turn-123",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"error":"failed to update goal: database locked"}`),
	}); err != nil {
		t.Fatal(err)
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Tasks[task.ID].PendingGoalStatus != "" {
		t.Fatalf("failed tool call marked the goal terminal: %+v", st.Tasks[task.ID])
	}
}

func TestDelegateFailureDoesNotLeaveWorkerBusy(t *testing.T) {
	a, fake, _ := testApp(t)
	fake.sendErr = fmt.Errorf("paste failed")
	if _, err := a.Delegate("worker-1", "Task"); err == nil {
		t.Fatal("expected delegation to fail")
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	worker := st.Workers["worker-1"]
	if worker == nil || worker.Busy {
		t.Fatalf("worker remained busy after failed paste: %+v", worker)
	}
	var failed *state.Task
	for _, task := range st.Tasks {
		failed = task
	}
	if failed == nil || failed.Status != state.TaskFailed {
		t.Fatalf("task was not marked failed: %+v", failed)
	}
}

func TestTerminalStopUsesOnlyThatStopsFinalMessage(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("worker-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("brain")
	if err := a.HandleHook("stop", HookInput{SessionID: "brain-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	intermediate := "intermediate response that must not become the handoff"
	fake.setCurrent("worker-1")
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace, LastAssistantMessage: &intermediate}); err != nil {
		t.Fatal(err)
	}
	if err := a.HandleHook("post-tool-use", HookInput{
		SessionID: "worker-thread", CWD: task.Workspace, TurnID: "terminal-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"complete"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace}); err != nil {
		t.Fatal(err)
	}

	prompts := fake.prompts()
	if len(prompts) != 2 {
		t.Fatalf("expected goal and handoff, got %d prompts", len(prompts))
	}
	if strings.Contains(prompts[1].text, intermediate) {
		t.Fatalf("stale intermediate message was relayed: %q", prompts[1].text)
	}
	if !strings.Contains(prompts[1].text, "Worker produced no final assistant message") {
		t.Fatalf("missing empty-final fallback: %q", prompts[1].text)
	}
}

func TestReserveOldestDeliveryIsFIFO(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	st := state.New("brain")
	st.Deliveries["newer"] = &state.Delivery{ID: "newer", Status: state.DeliveryPending, CreatedAt: now.Add(time.Minute)}
	st.Deliveries["older"] = &state.Delivery{ID: "older", Status: state.DeliveryPending, CreatedAt: now}
	reserved := reserveOldestDelivery(&st, now.Add(2*time.Minute))
	if reserved == nil || reserved.ID != "older" {
		t.Fatalf("expected oldest delivery, got %+v", reserved)
	}
}

func TestDeliverRequeuesWhenBrainStartedAnotherTurn(t *testing.T) {
	a, fake, clock := testApp(t)
	messagePath := filepath.Join(a.Paths.HandoffsDir, "handoff.md")
	if err := os.WriteFile(messagePath, []byte("handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["msg-one"] = &state.Delivery{
			ID: "msg-one", WorkerSession: "worker-1", Workspace: "/repo/worktrees/worker-1",
			GoalStatus: "complete", MessagePath: messagePath, Status: state.DeliverySending,
			CreatedAt: clock, ReservedAt: clock,
		}
		st.Brain.Busy = true
		st.Brain.TurnID = "new-turn"
		st.Brain.ReservedDelivery = "msg-one"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initialPromptCount := len(fake.prompts())
	if err := a.Deliver("msg-one", 0); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts()) != initialPromptCount {
		t.Fatal("handoff was injected into an active Brain turn")
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Deliveries["msg-one"].Status != state.DeliveryPending || st.Brain.ReservedDelivery != "" {
		t.Fatalf("delivery was not requeued: delivery=%+v brain=%+v", st.Deliveries["msg-one"], st.Brain)
	}
}

func TestForceFlushRequeuesAndDeliversCurrentReservation(t *testing.T) {
	a, fake, clock := testApp(t)
	messagePath := filepath.Join(a.Paths.HandoffsDir, "force-handoff.md")
	if err := os.WriteFile(messagePath, []byte("forced handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["msg-force"] = &state.Delivery{
			ID: "msg-force", WorkerSession: "worker-1", WorkerAlias: "worker-1",
			MessagePath: messagePath, Status: state.DeliverySending,
			CreatedAt: clock, ReservedAt: clock,
		}
		st.Brain.Busy = true
		st.Brain.TurnID = "stale-turn"
		st.Brain.ReservedDelivery = "msg-force"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initialPrompts := len(fake.prompts())
	deliveryID, err := a.Flush(true)
	if err != nil {
		t.Fatal(err)
	}
	if deliveryID != "msg-force" || len(fake.prompts()) != initialPrompts+1 {
		t.Fatalf("force flush did not deliver reservation: id=%q prompts=%+v", deliveryID, fake.prompts())
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Deliveries["msg-force"].Status != state.DeliveryDelivered || st.Brain.ReservedDelivery != "" {
		t.Fatalf("force flush left stale reservation: delivery=%+v brain=%+v", st.Deliveries["msg-force"], st.Brain)
	}
}

func TestForceFlushInvalidatesAStillSleepingDeliveryLease(t *testing.T) {
	a, fake, clock := testApp(t)
	messagePath := filepath.Join(a.Paths.HandoffsDir, "leased-handoff.md")
	if err := os.WriteFile(messagePath, []byte("single handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["msg-lease"] = &state.Delivery{
			ID: "msg-lease", WorkerSession: "worker-1", WorkerAlias: "worker-1",
			MessagePath: messagePath, Status: state.DeliverySending, Attempts: 1,
			CreatedAt: clock, ReservedAt: clock,
		}
		st.Brain.Busy = true
		st.Brain.ReservedDelivery = "msg-lease"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	oldResult := make(chan error, 1)
	go func() {
		close(started)
		oldResult <- a.DeliverLease("msg-lease", 1, 50*time.Millisecond)
	}()
	<-started
	initialPrompts := len(fake.prompts())
	if deliveryID, err := a.Flush(true); err != nil || deliveryID != "msg-lease" {
		t.Fatalf("force flush failed: id=%q err=%v", deliveryID, err)
	}
	<-oldResult
	if len(fake.prompts()) != initialPrompts+1 {
		t.Fatalf("handoff was pasted more than once: %+v", fake.prompts())
	}
}

func TestForceFlushRefusesDeliveryAlreadyBeingPasted(t *testing.T) {
	a, fake, clock := testApp(t)
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["msg-pasting"] = &state.Delivery{
			ID: "msg-pasting", Status: state.DeliveryPasting, Attempts: 1,
			CreatedAt: clock, ReservedAt: clock,
		}
		st.Brain.Busy = true
		st.Brain.ReservedDelivery = "msg-pasting"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initialPrompts := len(fake.prompts())
	if _, err := a.Flush(true); err == nil {
		t.Fatal("force flush replaced an in-flight paste lease")
	}
	if len(fake.prompts()) != initialPrompts {
		t.Fatal("force flush pasted while another helper owned the lease")
	}
}

func projectTestApp(t *testing.T, root, projectID string, fake *fakeTmux, clock time.Time) *App {
	t.Helper()
	base := config.Paths{
		Root: root, Home: root, ProjectsDir: filepath.Join(root, "projects"),
		Config: filepath.Join(root, "config.json"), State: filepath.Join(root, "state.json"), Lock: filepath.Join(root, "state.lock"),
		TasksDir: filepath.Join(root, "tasks"), HandoffsDir: filepath.Join(root, "handoffs"), CacheDir: filepath.Join(root, "cache"), LogsDir: filepath.Join(root, "logs"), Log: filepath.Join(root, "logs", "conductor.log"),
	}
	paths := config.ForProject(base, projectID)
	cfg := config.Default()
	cfg.DeliveryDelayMS = 0
	brainSession := projectID + "--brain"
	workerPattern := `^` + regexp.QuoteMeta(projectID+"--") + `worker-[1-9][0-9]*$`
	store := state.NewProjectStore(paths, projectID, brainSession)
	store.Now = func() time.Time { return clock }
	a := &App{
		ProjectID: projectID, BasePaths: base, Paths: paths, Config: cfg, BrainSession: brainSession,
		Store: store, Tmux: fake, Executable: "/tmp/conductor", Now: func() time.Time { return clock },
		workerRE: regexp.MustCompile(workerPattern),
	}
	if err := config.EnsureDirectories(paths); err != nil {
		t.Fatal(err)
	}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNamedProjectsRouteIdenticalWorkerAliasesIndependently(t *testing.T) {
	root := t.TempDir()
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeTmux{current: "project1--brain", panes: map[string]tmux.Pane{
		"project1--brain":    {ID: "%10", Session: "project1--brain", Path: "/repo/project1", Command: "codex", Active: true},
		"project1--worker-1": {ID: "%11", Session: "project1--worker-1", Path: "/repo/project1-wt/worker-1", Command: "codex", Active: true},
		"project2--brain":    {ID: "%20", Session: "project2--brain", Path: "/repo/project2", Command: "codex", Active: true},
		"project2--worker-1": {ID: "%21", Session: "project2--worker-1", Path: "/repo/project2-wt/worker-1", Command: "codex", Active: true},
	}}
	project1 := projectTestApp(t, root, "project1", fake, clock)
	project2 := projectTestApp(t, root, "project2", fake, clock)

	project1Task, err := project1.Delegate("worker-1", "Project1 task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project2--brain")
	project2Task, err := project2.Delegate("worker-1", "Project2 task")
	if err != nil {
		t.Fatal(err)
	}
	if project1Task.WorkerSession != "project1--worker-1" || project1Task.WorkerAlias != "worker-1" {
		t.Fatalf("unexpected project1 routing: %+v", project1Task)
	}
	if project2Task.WorkerSession != "project2--worker-1" || project2Task.WorkerAlias != "worker-1" {
		t.Fatalf("unexpected project2 routing: %+v", project2Task)
	}

	project1State, err := project1.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	project2State, err := project2.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveTaskForWorker(&project1State, "project2--worker-1") != nil || state.ActiveTaskForWorker(&project2State, "project1--worker-1") != nil {
		t.Fatal("project state leaked across namespaces")
	}
	if project1.Paths.State == project2.Paths.State {
		t.Fatalf("projects share a state file: %s", project1.Paths.State)
	}

	prompts := fake.prompts()
	if len(prompts) != 2 || prompts[0].target != "%11" || prompts[1].target != "%21" {
		t.Fatalf("goals crossed projects: %+v", prompts)
	}
}

func TestNamedProjectHandoffReturnsOnlyToItsBrain(t *testing.T) {
	root := t.TempDir()
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeTmux{current: "project1--brain", panes: map[string]tmux.Pane{
		"project1--brain":    {ID: "%10", Session: "project1--brain", Path: "/repo/project1", Command: "codex", Active: true},
		"project1--worker-1": {ID: "%11", Session: "project1--worker-1", Path: "/repo/project1-wt/worker-1", Command: "codex", Active: true},
		"project2--brain":    {ID: "%20", Session: "project2--brain", Path: "/repo/project2", Command: "codex", Active: true},
	}}
	project1 := projectTestApp(t, root, "project1", fake, clock)
	task, err := project1.Delegate("worker-1", "Investigate")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project1--brain")
	if err := project1.HandleHook("stop", HookInput{SessionID: "project1-brain-thread", CWD: "/repo/project1"}); err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project1--worker-1")
	if err := project1.HandleHook("post-tool-use", HookInput{
		SessionID: "project1-worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"complete"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	handoff := "Structured handoff chosen by Brain."
	if err := project1.HandleHook("stop", HookInput{SessionID: "project1-worker-thread", CWD: task.Workspace, TurnID: "worker-turn", LastAssistantMessage: &handoff}); err != nil {
		t.Fatal(err)
	}
	prompts := fake.prompts()
	if len(prompts) != 2 {
		t.Fatalf("expected goal and handoff, got %+v", prompts)
	}
	if prompts[1].target != "%10" {
		t.Fatalf("handoff targeted the wrong Brain pane: %+v", prompts[1])
	}
	if strings.Contains(prompts[1].text, "project1--worker-1") || !strings.Contains(prompts[1].text, "[CONDUCTOR HANDOFF | worker-1 | complete]") {
		t.Fatalf("handoff leaked transport namespace or lost alias: %q", prompts[1].text)
	}
}
