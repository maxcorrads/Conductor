package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/maxcorrads/conductor/internal/app"
	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/project"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
	"github.com/maxcorrads/conductor/internal/transcript"
)

const (
	guiSnapshotSchemaVersion = 4
	guiMaxRecentRecords      = 100
	guiMaxSelectedRecords    = 200
	guiMaxTotalTextBytes     = 16 * 1024 * 1024
)

type guiSnapshot struct {
	SchemaVersion    int                          `json:"schema_version"`
	ConductorVersion string                       `json:"conductor_version"`
	GeneratedAt      time.Time                    `json:"generated_at"`
	ConductorHome    string                       `json:"conductor_home"`
	Executable       string                       `json:"executable"`
	TmuxExecutable   string                       `json:"tmux_executable"`
	TmuxSessions     []string                     `json:"tmux_sessions"`
	SessionActivity  map[string]bool              `json:"session_activity"`
	SessionAttention map[string]bool              `json:"session_attention"`
	SessionProfiles  map[string]guiSessionProfile `json:"session_profiles"`
	TmuxError        string                       `json:"tmux_error,omitempty"`
	Projects         []guiProjectSnapshot         `json:"projects"`
}

type guiSessionProfile struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type guiSessionSnapshot struct {
	SchemaVersion    int             `json:"schema_version"`
	GeneratedAt      time.Time       `json:"generated_at"`
	TmuxExecutable   string          `json:"tmux_executable"`
	TmuxSessions     []string        `json:"tmux_sessions"`
	SessionActivity  map[string]bool `json:"session_activity"`
	SessionAttention map[string]bool `json:"session_attention"`
	TmuxError        string          `json:"tmux_error,omitempty"`
}

type guiModelCatalog struct {
	SchemaVersion   int             `json:"schema_version"`
	CodexExecutable string          `json:"codex_executable,omitempty"`
	Models          []guiCodexModel `json:"models"`
	Error           string          `json:"error,omitempty"`
}

type guiCodexModel struct {
	Slug                  string                   `json:"slug"`
	DisplayName           string                   `json:"display_name"`
	Visibility            string                   `json:"visibility"`
	DefaultReasoningLevel string                   `json:"default_reasoning_level"`
	SupportedReasoning    []guiCodexReasoningLevel `json:"supported_reasoning_levels"`
}

type guiCodexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type guiProjectSnapshot struct {
	ID                    string            `json:"id"`
	BrainSession          string            `json:"brain_session"`
	StatePath             string            `json:"state_path"`
	LogPath               string            `json:"log_path"`
	WorkerSessions        []string          `json:"worker_sessions"`
	WorkerSessionTemplate string            `json:"worker_session_template"`
	TaskCount             int               `json:"task_count"`
	HandoffCount          int               `json:"handoff_count"`
	HistoryTruncated      bool              `json:"history_truncated"`
	State                 state.State       `json:"state"`
	TaskOrder             []string          `json:"task_order"`
	HandoffOrder          []string          `json:"handoff_order"`
	GoalTexts             map[string]string `json:"goal_texts"`
	GoalTextTruncated     map[string]bool   `json:"goal_text_truncated"`
	HandoffMessages       map[string]string `json:"handoff_messages"`
	HandoffTextTruncated  map[string]bool   `json:"handoff_message_truncated"`
	LogTail               string            `json:"log_tail"`
}

func runGUI(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: conductor gui <snapshot|sessions|models>")
	}
	if args[0] == "models" {
		catalog := buildGUIModelCatalog()
		data, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if args[0] == "sessions" {
		probe, err := buildGUISessionSnapshot()
		if err != nil {
			return err
		}
		if probe.TmuxSessions == nil {
			probe.TmuxSessions = []string{}
		}
		data, err := json.MarshalIndent(probe, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if args[0] != "snapshot" {
		return errors.New("usage: conductor gui <snapshot|sessions|models>")
	}
	snapshot, err := buildGUISnapshot()
	if err != nil {
		return err
	}
	snapshot.normalizeCollections()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func buildGUIModelCatalog() guiModelCatalog {
	catalog := guiModelCatalog{
		SchemaVersion: guiSnapshotSchemaVersion,
		Models:        []guiCodexModel{},
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		catalog.Error = "Codex CLI was not found; enter a model ID manually."
		return catalog
	}
	catalog.CodexExecutable = executable
	models, err := resolveGUIModelCatalog(
		func() ([]guiCodexModel, error) { return loadGUIModelCatalog(executable) },
		func() ([]guiCodexModel, error) { return loadGUIModelCatalog(executable, "--bundled") },
	)
	if err != nil {
		catalog.Error = "The installed Codex CLI did not provide a model catalog; enter a model ID manually."
		return catalog
	}
	catalog.Models = models
	return catalog
}

func resolveGUIModelCatalog(
	live func() ([]guiCodexModel, error),
	bundled func() ([]guiCodexModel, error),
) ([]guiCodexModel, error) {
	models, err := live()
	if err == nil {
		return models, nil
	}
	return bundled()
}

func loadGUIModelCatalog(executable string, extraArgs ...string) ([]guiCodexModel, error) {
	args := append([]string{"debug", "models"}, extraArgs...)
	output, err := exec.Command(executable, args...).Output()
	if err != nil {
		return nil, err
	}
	models, err := decodeGUIModelCatalog(output)
	if err != nil {
		return nil, err
	}
	return visibleGUIModels(models), nil
}

func visibleGUIModels(models []guiCodexModel) []guiCodexModel {
	visible := make([]guiCodexModel, 0, len(models))
	for _, model := range models {
		if model.Visibility == "list" {
			visible = append(visible, model)
		}
	}
	return visible
}

func decodeGUIModelCatalog(output []byte) ([]guiCodexModel, error) {
	var models []guiCodexModel
	if err := json.Unmarshal(output, &models); err == nil {
		if models == nil {
			models = []guiCodexModel{}
		}
		return models, nil
	}
	var wrapped struct {
		Models []guiCodexModel `json:"models"`
	}
	if err := json.Unmarshal(output, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Models == nil {
		wrapped.Models = []guiCodexModel{}
	}
	return wrapped.Models, nil
}

func buildGUISessionSnapshot() (guiSessionSnapshot, error) {
	projectApp, err := app.NewForProject("default")
	if err != nil {
		return guiSessionSnapshot{}, err
	}
	probe := guiSessionSnapshot{
		SchemaVersion:    guiSnapshotSchemaVersion,
		GeneratedAt:      time.Now().UTC(),
		TmuxExecutable:   projectApp.Config.TmuxCommand,
		TmuxSessions:     []string{},
		SessionActivity:  map[string]bool{},
		SessionAttention: map[string]bool{},
	}
	if resolvedTmux, resolveErr := exec.LookPath(projectApp.Config.TmuxCommand); resolveErr == nil {
		probe.TmuxExecutable = resolvedTmux
	}
	sessions, sessionErr := projectApp.Tmux.ListSessions()
	if sessionErr != nil {
		probe.TmuxError = sessionErr.Error()
	} else {
		sort.Strings(sessions)
		probe.TmuxSessions = sessions
		probe.SessionActivity, probe.SessionAttention = detectSessionSignals(
			projectApp.Config.TmuxCommand,
			conductorSessions(sessions, projectApp.Config),
		)
	}
	return probe, nil
}

func buildGUISnapshot() (guiSnapshot, error) {
	ids, err := app.DiscoverProjectIDs()
	if err != nil {
		return guiSnapshot{}, err
	}
	snapshot := guiSnapshot{
		SchemaVersion:    guiSnapshotSchemaVersion,
		ConductorVersion: version,
		GeneratedAt:      time.Now().UTC(),
		TmuxSessions:     []string{},
		SessionActivity:  map[string]bool{},
		SessionAttention: map[string]bool{},
		SessionProfiles:  map[string]guiSessionProfile{},
		Projects:         make([]guiProjectSnapshot, 0, len(ids)),
	}
	sessionCodexIDs := map[string]string{}
	sessionCreatedAt := map[string]time.Time{}
	sessionPanes := map[string]tmux.Pane{}
	projectTextBudget := int64(guiMaxTotalTextBytes)
	if len(ids) > 0 {
		projectTextBudget /= int64(len(ids))
	}
	for index, id := range ids {
		projectApp, appErr := app.NewForProject(id)
		if appErr != nil {
			return guiSnapshot{}, appErr
		}
		if index == 0 {
			snapshot.ConductorHome = projectApp.BasePaths.Root
			snapshot.Executable = projectApp.Executable
			snapshot.TmuxExecutable = projectApp.Config.TmuxCommand
			if resolvedTmux, resolveErr := exec.LookPath(projectApp.Config.TmuxCommand); resolveErr == nil {
				snapshot.TmuxExecutable = resolvedTmux
			}
			sessions, sessionErr := projectApp.Tmux.ListSessions()
			if sessionErr != nil {
				snapshot.TmuxError = sessionErr.Error()
			} else {
				sort.Strings(sessions)
				snapshot.TmuxSessions = sessions
				if execTmux, ok := projectApp.Tmux.(*tmux.ExecClient); ok {
					if created, createdErr := execTmux.SessionCreatedAt(); createdErr == nil {
						sessionCreatedAt = created
					}
					if panes, panesErr := execTmux.SessionPanes(); panesErr == nil {
						sessionPanes = panes
					}
				}
				snapshot.SessionActivity, snapshot.SessionAttention = detectSessionSignals(
					projectApp.Config.TmuxCommand,
					conductorSessions(sessions, projectApp.Config),
				)
			}
		}
		st, readErr := projectApp.Store.Read()
		if readErr != nil {
			return guiSnapshot{}, readErr
		}
		if st.Brain.CodexSessionID != "" {
			brainPane := sessionPanes[projectApp.BrainSession]
			if sessionProfileBindingIsCurrent(projectApp.BrainSession, st.Brain.Pane, brainPane, st.Brain.CodexSessionObservedAt, st.Brain.TmuxSessionCreatedAt, sessionCreatedAt) {
				sessionCodexIDs[projectApp.BrainSession] = st.Brain.CodexSessionID
			}
		}
		for session, worker := range st.Workers {
			if worker == nil || worker.CodexSessionID == "" {
				continue
			}
			workerPane := sessionPanes[session]
			if sessionProfileBindingIsCurrent(session, worker.Pane, workerPane, worker.CodexSessionObservedAt, worker.TmuxSessionCreatedAt, sessionCreatedAt) {
				sessionCodexIDs[session] = worker.CodexSessionID
			}
		}
		tasks := make([]*state.Task, 0, len(st.Tasks))
		for _, task := range st.Tasks {
			tasks = append(tasks, task)
		}
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
		selectedTasks := selectRecentTasks(tasks)

		deliveries := make([]*state.Delivery, 0, len(st.Deliveries))
		for _, delivery := range st.Deliveries {
			deliveries = append(deliveries, delivery)
		}
		sort.Slice(deliveries, func(i, j int) bool { return deliveries[i].CreatedAt.After(deliveries[j].CreatedAt) })
		selectedDeliveries := selectRecentDeliveries(deliveries)

		displayState := st
		applyLiveActivity(&displayState, projectApp.BrainSession, snapshot.SessionActivity)
		displayState.Tasks = make(map[string]*state.Task, len(selectedTasks))
		for _, task := range selectedTasks {
			displayState.Tasks[task.ID] = task
		}
		displayState.Deliveries = make(map[string]*state.Delivery, len(selectedDeliveries))
		for _, delivery := range selectedDeliveries {
			displayState.Deliveries[delivery.ID] = delivery
		}

		projectSnapshot := guiProjectSnapshot{
			ID:                   id,
			BrainSession:         projectApp.BrainSession,
			StatePath:            projectApp.Paths.State,
			LogPath:              projectApp.Paths.Log,
			WorkerSessions:       []string{},
			TaskCount:            len(st.Tasks),
			HandoffCount:         len(st.Deliveries),
			HistoryTruncated:     len(selectedTasks) < len(st.Tasks) || len(selectedDeliveries) < len(st.Deliveries),
			State:                displayState,
			GoalTexts:            make(map[string]string),
			GoalTextTruncated:    make(map[string]bool),
			HandoffMessages:      make(map[string]string),
			HandoffTextTruncated: make(map[string]bool),
		}
		projectSnapshot.WorkerSessionTemplate = guiWorkerSessionTemplate(id, projectApp.Config.WorkerSessionPattern)
		workerSessions := make(map[string]struct{})
		for _, session := range snapshot.TmuxSessions {
			workerSessions[session] = struct{}{}
		}
		for session := range st.Workers {
			workerSessions[session] = struct{}{}
		}
		for _, task := range st.Tasks {
			workerSessions[task.WorkerSession] = struct{}{}
		}
		for session := range workerSessions {
			if projectApp.IsWorkerSession(session) {
				projectSnapshot.WorkerSessions = append(projectSnapshot.WorkerSessions, session)
			}
		}
		sort.Strings(projectSnapshot.WorkerSessions)

		for _, task := range selectedTasks {
			projectSnapshot.TaskOrder = append(projectSnapshot.TaskOrder, task.ID)
		}

		for _, delivery := range selectedDeliveries {
			projectSnapshot.HandoffOrder = append(projectSnapshot.HandoffOrder, delivery.ID)
		}
		populateProjectText(&projectSnapshot, selectedTasks, selectedDeliveries, projectApp.Paths.Log, projectTextBudget)
		snapshot.Projects = append(snapshot.Projects, projectSnapshot)
	}
	threadIDs := make([]string, 0, len(sessionCodexIDs))
	for _, threadID := range sessionCodexIDs {
		threadIDs = append(threadIDs, threadID)
	}
	if profiles, _, profileErr := transcript.SessionProfilesFromSQLiteChecked(threadIDs); profileErr == nil {
		for session, threadID := range sessionCodexIDs {
			if profile, found := profiles[threadID]; found {
				snapshot.SessionProfiles[session] = guiSessionProfile{Model: profile.Model, Effort: profile.Effort}
			}
		}
	}
	return snapshot, nil
}

func sessionBindingIsCurrent(session string, bindingObservedAt time.Time, sessionCreatedAt map[string]time.Time) bool {
	createdAt, found := sessionCreatedAt[session]
	return found && !bindingObservedAt.IsZero() && !bindingObservedAt.Before(createdAt)
}

func sessionProfileBindingIsCurrent(session, storedPane string, livePane tmux.Pane, bindingObservedAt, bindingSessionCreatedAt time.Time, sessionCreatedAt map[string]time.Time) bool {
	if !sessionBindingIsCurrent(session, bindingObservedAt, sessionCreatedAt) {
		return false
	}
	liveCreatedAt, found := sessionCreatedAt[session]
	if !found || bindingSessionCreatedAt.IsZero() || !bindingSessionCreatedAt.Equal(liveCreatedAt) || storedPane == "" || livePane.ID != storedPane {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(livePane.Command))
	if slash := strings.LastIndex(command, "/"); slash >= 0 {
		command = command[slash+1:]
	}
	return command == "codex" || strings.HasPrefix(command, "codex-")
}

func detectSessionSignals(tmuxCommand string, sessions []string) (map[string]bool, map[string]bool) {
	activity := make(map[string]bool, len(sessions))
	attention := make(map[string]bool, len(sessions))
	if len(sessions) == 0 {
		return activity, attention
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return activity, attention
	}
	nonce := hex.EncodeToString(nonceBytes)
	args := make([]string, 0, len(sessions)*14)
	beginMarkers := make([]string, len(sessions))
	endMarkers := make([]string, len(sessions))
	for index, session := range sessions {
		beginMarkers[index] = fmt.Sprintf("__CONDUCTOR_ACTIVITY_%s_%d_BEGIN__", nonce, index)
		endMarkers[index] = fmt.Sprintf("__CONDUCTOR_ACTIVITY_%s_%d_END__", nonce, index)
		if index > 0 {
			args = append(args, ";")
		}
		args = append(args,
			"display-message", "-p", "-t", session, beginMarkers[index],
			";", "capture-pane", "-p", "-t", session, "-S", "-30",
			";", "display-message", "-p", "-t", session, endMarkers[index],
		)
	}
	output, err := exec.Command(tmuxCommand, args...).CombinedOutput()
	if err != nil {
		// A pane may disappear between list-sessions and this batch. In that case,
		// keep the authoritative hook state instead of projecting every later
		// session as idle after tmux aborts the remaining commands.
		return activity, attention
	}
	content := string(output)
	for index, session := range sessions {
		start := strings.Index(content, beginMarkers[index]+"\n")
		if start < 0 {
			continue
		}
		start += len(beginMarkers[index]) + 1
		endOffset := strings.Index(content[start:], "\n"+endMarkers[index]+"\n")
		if endOffset < 0 {
			continue
		}
		end := start + endOffset
		pane := content[start:end]
		active := paneShowsActiveTurn(pane)
		activity[session] = active
		attention[session] = !active && tmux.PaneShowsReplaceGoalPrompt(pane)
	}
	return activity, attention
}

func conductorSessions(sessions []string, cfg config.Config) []string {
	tracked := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if _, ok := project.ParseSession(session, cfg.BrainSession, cfg.WorkerSessionPattern); ok {
			tracked = append(tracked, session)
		}
	}
	return tracked
}

func paneShowsActiveTurn(content string) bool {
	return tmux.PaneShowsActiveTurn(content)
}

func paneShowsEmptyComposer(content string) bool {
	lines := strings.Split(strings.ToLower(content), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		for _, prefix := range []string{"›", "❯", ">"} {
			if strings.HasPrefix(line, prefix) {
				composer := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				return composer == "ask codex to do anything"
			}
		}
	}
	return false
}

func applyLiveActivity(display *state.State, brainSession string, live map[string]bool) {
	if display == nil {
		return
	}
	if busy, ok := live[brainSession]; ok {
		display.Brain.Busy = busy
	}
	workers := make(map[string]*state.Worker, len(display.Workers))
	for session, worker := range display.Workers {
		if worker == nil {
			workers[session] = nil
			continue
		}
		copyWorker := *worker
		if busy, ok := live[session]; ok {
			copyWorker.Busy = busy
		}
		workers[session] = &copyWorker
	}
	display.Workers = workers
}

func populateProjectText(project *guiProjectSnapshot, tasks []*state.Task, deliveries []*state.Delivery, logPath string, totalBudget int64) {
	if project == nil || totalBudget <= 0 {
		return
	}
	logBudget := minInt64(256*1024, totalBudget/16)
	contentBudget := totalBudget - logBudget
	handoffBudget := contentBudget / 2
	goalBudget := contentBudget - handoffBudget

	if logBudget > 0 {
		project.LogTail, _ = readTextTail(logPath, logBudget)
	}

	prioritizedDeliveries := append([]*state.Delivery(nil), deliveries...)
	sort.SliceStable(prioritizedDeliveries, func(i, j int) bool {
		return prioritizedDeliveries[i].Status == state.DeliveryPending && prioritizedDeliveries[j].Status != state.DeliveryPending
	})
	for index, delivery := range prioritizedDeliveries {
		remainingRecords := int64(len(prioritizedDeliveries) - index)
		limit := minInt64(2*1024*1024, handoffBudget/remainingRecords)
		if limit <= 0 {
			break
		}
		if text, truncated, fileErr := readTextPrefix(delivery.MessagePath, limit); fileErr == nil {
			project.HandoffMessages[delivery.ID] = text
			project.HandoffTextTruncated[delivery.ID] = truncated
			handoffBudget -= int64(len(text))
		}
	}

	for index, task := range tasks {
		remainingRecords := int64(len(tasks) - index)
		limit := minInt64(1024*1024, goalBudget/remainingRecords)
		if limit <= 0 {
			break
		}
		if text, truncated, fileErr := readTextPrefix(task.ObjectivePath, limit); fileErr == nil {
			project.GoalTexts[task.ID] = text
			project.GoalTextTruncated[task.ID] = truncated
			goalBudget -= int64(len(text))
		}
	}
}

func guiWorkerSessionTemplate(projectID, defaultPattern string) string {
	if projectID != "default" {
		return projectID + "--worker-%d"
	}
	if defaultPattern == config.Default().WorkerSessionPattern {
		return "worker-%d"
	}
	return ""
}

func selectRecentTasks(tasks []*state.Task) []*state.Task {
	selected := append([]*state.Task(nil), tasks[:minInt(len(tasks), guiMaxRecentRecords)]...)
	seen := make(map[string]struct{}, len(selected))
	for _, task := range selected {
		seen[task.ID] = struct{}{}
	}
	for _, task := range tasks {
		if len(selected) >= guiMaxSelectedRecords {
			break
		}
		if task.Status == state.TaskRunning {
			if _, ok := seen[task.ID]; !ok {
				selected = append(selected, task)
				seen[task.ID] = struct{}{}
			}
		}
	}
	return selected
}

func selectRecentDeliveries(deliveries []*state.Delivery) []*state.Delivery {
	selected := append([]*state.Delivery(nil), deliveries[:minInt(len(deliveries), guiMaxRecentRecords)]...)
	seen := make(map[string]struct{}, len(selected))
	for _, delivery := range selected {
		seen[delivery.ID] = struct{}{}
	}
	for _, delivery := range deliveries {
		if len(selected) >= guiMaxSelectedRecords {
			break
		}
		if delivery.Status == state.DeliveryPending {
			if _, ok := seen[delivery.ID]; !ok {
				selected = append(selected, delivery)
				seen[delivery.ID] = struct{}{}
			}
		}
	}
	return selected
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func (snapshot *guiSnapshot) normalizeCollections() {
	if snapshot.TmuxSessions == nil {
		snapshot.TmuxSessions = []string{}
	}
	if snapshot.SessionActivity == nil {
		snapshot.SessionActivity = map[string]bool{}
	}
	if snapshot.SessionAttention == nil {
		snapshot.SessionAttention = map[string]bool{}
	}
	if snapshot.SessionProfiles == nil {
		snapshot.SessionProfiles = map[string]guiSessionProfile{}
	}
	if snapshot.Projects == nil {
		snapshot.Projects = []guiProjectSnapshot{}
	}
	for index := range snapshot.Projects {
		project := &snapshot.Projects[index]
		if project.TaskOrder == nil {
			project.TaskOrder = []string{}
		}
		if project.WorkerSessions == nil {
			project.WorkerSessions = []string{}
		}
		if project.HandoffOrder == nil {
			project.HandoffOrder = []string{}
		}
		if project.GoalTexts == nil {
			project.GoalTexts = map[string]string{}
		}
		if project.GoalTextTruncated == nil {
			project.GoalTextTruncated = map[string]bool{}
		}
		if project.HandoffMessages == nil {
			project.HandoffMessages = map[string]string{}
		}
		if project.HandoffTextTruncated == nil {
			project.HandoffTextTruncated = map[string]bool{}
		}
	}
}

func readTextPrefix(path string, limit int64) (string, bool, error) {
	if path == "" {
		return "", false, os.ErrNotExist
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	return string(data), info.Size() > int64(len(data)), err
}

func readTextTail(path string, limit int64) (string, error) {
	if path == "" {
		return "", os.ErrNotExist
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > limit {
		if _, err := file.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return "", err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	return string(data), err
}
