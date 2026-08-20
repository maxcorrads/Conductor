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
	fake := &fakeTmux{current: "sol", panes: map[string]tmux.Pane{
		"sol":    {ID: "%1", Session: "sol", Path: "/repo/main", Command: "codex", Active: true},
		"luna-1": {ID: "%2", Session: "luna-1", Path: "/repo/worktrees/luna-1", Command: "codex", Active: true},
		"luna-2": {ID: "%3", Session: "luna-2", Path: "/repo/worktrees/luna-2", Command: "codex", Active: true},
	}}
	store := state.NewStore(paths, cfg.SolSession)
	store.Now = func() time.Time { return clock }
	a := &App{ProjectID: "default", Paths: paths, Config: cfg, SolSession: cfg.SolSession, Store: store, Tmux: fake, Executable: "/tmp/conductor", Now: func() time.Time { return clock }, workerRE: regexp.MustCompile(cfg.WorkerSessionPattern)}
	if err := config.EnsureDirectories(paths); err != nil {
		t.Fatal(err)
	}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	return a, fake, clock
}

func TestDelegateSendsExactInlineObjective(t *testing.T) {
	a, fake, _ := testApp(t)
	objective := "  Investigate without edits.\n\nReturn any handoff structure Sol requested.  "
	task, err := a.Delegate("luna-1", objective)
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
	task, err := a.Delegate("luna-1", objective)
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
	first, err := a.Delegate("luna-1", "Investigate path A")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Delegate("luna-2", "Investigate path B")
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
	if state.ActiveTaskForWorker(&st, "luna-1") == nil || state.ActiveTaskForWorker(&st, "luna-2") == nil {
		t.Fatalf("parallel active tasks missing: %+v", st.Tasks)
	}
}

func TestVerbatimWorkerHandoffIsPastedIntoSol(t *testing.T) {
	a, fake, _ := testApp(t)
	task, err := a.Delegate("luna-1", "Investigate the race. Return any handoff structure you consider useful.")
	if err != nil {
		t.Fatal(err)
	}
	if prompts := fake.prompts(); len(prompts) != 1 || !strings.HasPrefix(prompts[0].text, "/goal ") {
		t.Fatalf("goal was not sent: %+v", prompts)
	}

	// Sol finishes its delegation turn and becomes truly idle.
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main", TurnID: "sol-turn"}); err != nil {
		t.Fatal(err)
	}

	fake.setCurrent("luna-1")
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
	prefix := "[CONDUCTOR HANDOFF | luna-1 | complete]\nworkspace: " + task.Workspace + "\n\n"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("missing envelope: %q", got)
	}
	if got[len(prefix):] != handoff {
		t.Fatalf("handoff was rewritten\nwant: %q\n got: %q", handoff, got[len(prefix):])
	}
}

func TestActiveGoalDoesNotWakeSol(t *testing.T) {
	a, fake, clock := testApp(t)
	task, err := a.Delegate("luna-1", "Long task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(a.Paths.Home, "active.jsonl")
	line := fmt.Sprintf("{\"timestamp\":%q,\"payload\":{\"type\":\"thread_goal_updated\",\"threadId\":\"worker-thread\",\"goal\":{\"objective\":%q,\"status\":\"active\"}}}\n", clock.Add(time.Minute).Format(time.RFC3339), task.SentGoalObjective)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := "Intermediate checkpoint"
	fake.setCurrent("luna-1")
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

func TestBusySolQueuesThenFlushesOneHandoff(t *testing.T) {
	a, fake, clock := testApp(t)
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Paths.Home, "complete.jsonl")
	line := fmt.Sprintf("{\"timestamp\":%q,\"payload\":{\"type\":\"thread_goal_updated\",\"threadId\":\"worker-thread\",\"goal\":{\"objective\":%q,\"status\":\"blocked\"}}}\n", clock.Add(time.Minute).Format(time.RFC3339), task.SentGoalObjective)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := "Blocked because a product decision is required. Options: A or B."
	fake.setCurrent("luna-1")
	if err := a.HandleHook("stop", HookInput{SessionID: "worker-thread", CWD: task.Workspace, TranscriptPath: &path, LastAssistantMessage: &msg}); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts()) != 1 {
		t.Fatal("busy Sol should not be interrupted")
	}
	st, _ := a.Store.Read()
	if len(state.PendingDeliveries(&st)) != 1 {
		t.Fatalf("handoff not queued: %+v", st.Deliveries)
	}
	if err := a.MarkSolIdle(); err != nil {
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
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("luna-1")
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
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	fake.setCurrent("luna-1")
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
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("luna-1")
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
	if _, err := a.Delegate("luna-1", "Task"); err == nil {
		t.Fatal("expected delegation to fail")
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	worker := st.Workers["luna-1"]
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
	task, err := a.Delegate("luna-1", "Task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("sol")
	if err := a.HandleHook("stop", HookInput{SessionID: "sol-thread", CWD: "/repo/main"}); err != nil {
		t.Fatal(err)
	}

	intermediate := "intermediate response that must not become the handoff"
	fake.setCurrent("luna-1")
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
	if !strings.Contains(prompts[1].text, "Luna produced no final assistant message") {
		t.Fatalf("missing empty-final fallback: %q", prompts[1].text)
	}
}

func TestReserveOldestDeliveryIsFIFO(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	st := state.New("sol")
	st.Deliveries["newer"] = &state.Delivery{ID: "newer", Status: state.DeliveryPending, CreatedAt: now.Add(time.Minute)}
	st.Deliveries["older"] = &state.Delivery{ID: "older", Status: state.DeliveryPending, CreatedAt: now}
	reserved := reserveOldestDelivery(&st, now.Add(2*time.Minute))
	if reserved == nil || reserved.ID != "older" {
		t.Fatalf("expected oldest delivery, got %+v", reserved)
	}
}

func TestDeliverRequeuesWhenSolStartedAnotherTurn(t *testing.T) {
	a, fake, clock := testApp(t)
	messagePath := filepath.Join(a.Paths.HandoffsDir, "handoff.md")
	if err := os.WriteFile(messagePath, []byte("handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(func(st *state.State) error {
		st.Deliveries["msg-one"] = &state.Delivery{
			ID: "msg-one", WorkerSession: "luna-1", Workspace: "/repo/worktrees/luna-1",
			GoalStatus: "complete", MessagePath: messagePath, Status: state.DeliverySending,
			CreatedAt: clock, ReservedAt: clock,
		}
		st.Sol.Busy = true
		st.Sol.TurnID = "new-turn"
		st.Sol.ReservedDelivery = "msg-one"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initialPromptCount := len(fake.prompts())
	if err := a.Deliver("msg-one", 0); err != nil {
		t.Fatal(err)
	}
	if len(fake.prompts()) != initialPromptCount {
		t.Fatal("handoff was injected into an active Sol turn")
	}
	st, err := a.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if st.Deliveries["msg-one"].Status != state.DeliveryPending || st.Sol.ReservedDelivery != "" {
		t.Fatalf("delivery was not requeued: delivery=%+v sol=%+v", st.Deliveries["msg-one"], st.Sol)
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
	solSession := projectID + "--sol"
	workerPattern := `^` + regexp.QuoteMeta(projectID+"--") + `luna-[1-9][0-9]*$`
	store := state.NewProjectStore(paths, projectID, solSession)
	store.Now = func() time.Time { return clock }
	a := &App{
		ProjectID: projectID, BasePaths: base, Paths: paths, Config: cfg, SolSession: solSession,
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
	fake := &fakeTmux{current: "project1--sol", panes: map[string]tmux.Pane{
		"project1--sol":    {ID: "%10", Session: "project1--sol", Path: "/repo/project1", Command: "codex", Active: true},
		"project1--luna-1": {ID: "%11", Session: "project1--luna-1", Path: "/repo/project1-wt/luna-1", Command: "codex", Active: true},
		"project2--sol":    {ID: "%20", Session: "project2--sol", Path: "/repo/project2", Command: "codex", Active: true},
		"project2--luna-1": {ID: "%21", Session: "project2--luna-1", Path: "/repo/project2-wt/luna-1", Command: "codex", Active: true},
	}}
	project1 := projectTestApp(t, root, "project1", fake, clock)
	project2 := projectTestApp(t, root, "project2", fake, clock)

	project1Task, err := project1.Delegate("luna-1", "Project1 task")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project2--sol")
	project2Task, err := project2.Delegate("luna-1", "Project2 task")
	if err != nil {
		t.Fatal(err)
	}
	if project1Task.WorkerSession != "project1--luna-1" || project1Task.WorkerAlias != "luna-1" {
		t.Fatalf("unexpected project1 routing: %+v", project1Task)
	}
	if project2Task.WorkerSession != "project2--luna-1" || project2Task.WorkerAlias != "luna-1" {
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
	if state.ActiveTaskForWorker(&project1State, "project2--luna-1") != nil || state.ActiveTaskForWorker(&project2State, "project1--luna-1") != nil {
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

func TestNamedProjectHandoffReturnsOnlyToItsSol(t *testing.T) {
	root := t.TempDir()
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeTmux{current: "project1--sol", panes: map[string]tmux.Pane{
		"project1--sol":    {ID: "%10", Session: "project1--sol", Path: "/repo/project1", Command: "codex", Active: true},
		"project1--luna-1": {ID: "%11", Session: "project1--luna-1", Path: "/repo/project1-wt/luna-1", Command: "codex", Active: true},
		"project2--sol":    {ID: "%20", Session: "project2--sol", Path: "/repo/project2", Command: "codex", Active: true},
	}}
	project1 := projectTestApp(t, root, "project1", fake, clock)
	task, err := project1.Delegate("luna-1", "Investigate")
	if err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project1--sol")
	if err := project1.HandleHook("stop", HookInput{SessionID: "project1-sol-thread", CWD: "/repo/project1"}); err != nil {
		t.Fatal(err)
	}
	fake.setCurrent("project1--luna-1")
	if err := project1.HandleHook("post-tool-use", HookInput{
		SessionID: "project1-worker-thread", CWD: task.Workspace, TurnID: "worker-turn",
		ToolName: "update_goal", ToolInput: json.RawMessage(`{"status":"complete"}`),
		ToolResponse: json.RawMessage(`{"goal":{"status":"complete"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	handoff := "Structured handoff chosen by Sol."
	if err := project1.HandleHook("stop", HookInput{SessionID: "project1-worker-thread", CWD: task.Workspace, TurnID: "worker-turn", LastAssistantMessage: &handoff}); err != nil {
		t.Fatal(err)
	}
	prompts := fake.prompts()
	if len(prompts) != 2 {
		t.Fatalf("expected goal and handoff, got %+v", prompts)
	}
	if prompts[1].target != "%10" {
		t.Fatalf("handoff targeted the wrong Sol pane: %+v", prompts[1])
	}
	if strings.Contains(prompts[1].text, "project1--luna-1") || !strings.Contains(prompts[1].text, "[CONDUCTOR HANDOFF | luna-1 | complete]") {
		t.Fatalf("handoff leaked transport namespace or lost alias: %q", prompts[1].text)
	}
}
