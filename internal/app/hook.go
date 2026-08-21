package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/transcript"
)

type HookInput struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       *string         `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Model                string          `json:"model"`
	TurnID               string          `json:"turn_id"`
	Prompt               string          `json:"prompt"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage *string         `json:"last_assistant_message"`
	ToolName             string          `json:"tool_name"`
	ToolUseID            string          `json:"tool_use_id"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
}

type roleKind int

const (
	roleUnknown roleKind = iota
	roleSol
	roleWorker
)

type resolvedRole struct {
	Kind    roleKind
	Session string
	Pane    string
}

func DecodeHookInput(data []byte) (HookInput, error) {
	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return HookInput{}, err
	}
	return input, nil
}

func (a *App) HandleHook(kind string, input HookInput) error {
	role := a.resolveRole(input)
	switch kind {
	case "session-start":
		return a.handleSessionStart(role, input)
	case "user-prompt-submit":
		return a.handlePromptSubmit(role, input)
	case "post-tool-use":
		return a.handlePostToolUse(role, input)
	case "stop":
		if role.Kind == roleSol {
			return a.handleSolStop(role, input)
		}
		if role.Kind == roleWorker {
			return a.handleWorkerStop(role, input)
		}
		return nil
	default:
		return fmt.Errorf("unknown hook %q", kind)
	}
}

func (a *App) handlePostToolUse(role resolvedRole, input HookInput) error {
	if role.Kind != roleWorker || input.ToolName != "update_goal" {
		return nil
	}
	status, ok := goalStatusFromToolInput(input.ToolInput)
	if !ok {
		return nil
	}
	// PostToolUse also fires for failed local function calls. When Codex provides
	// a response, require the goal object in that response to confirm the status;
	// otherwise a failed update_goal attempt could alter task lifecycle state.
	if len(input.ToolResponse) > 0 && string(input.ToolResponse) != "null" {
		confirmed, confirmedOK := goalStatusFromToolResponse(input.ToolResponse)
		if !confirmedOK || confirmed != status {
			return nil
		}
	}
	now := a.Now().UTC()
	return a.Store.Update(func(st *state.State) error {
		task := state.ActiveTaskForWorker(st, role.Session)
		if task == nil {
			return nil
		}
		task.ObservedGoalStatus = status
		task.GoalObservedAt = now
		task.ReconcileToken = ""
		task.LastError = ""
		if a.isTerminalGoalStatus(status) {
			task.PendingGoalStatus = status
			task.PendingGoalTurnID = input.TurnID
			task.PendingGoalAt = now
		} else {
			task.PendingGoalStatus = ""
			task.PendingGoalTurnID = ""
			task.PendingGoalAt = time.Time{}
		}
		task.CodexSessionID = input.SessionID
		task.UpdatedAt = now
		worker := st.Workers[role.Session]
		if worker == nil {
			worker = &state.Worker{Session: role.Session}
			st.Workers[role.Session] = worker
		}
		worker.Pane = role.Pane
		worker.CodexSessionID = input.SessionID
		worker.CWD = input.CWD
		worker.Busy = true
		worker.UpdatedAt = now
		return nil
	})
}

func goalStatusFromToolInput(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", false
	}
	status := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input.Status), "-", "_"))
	return status, status != ""
}

func goalStatusFromToolResponse(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return "", false
	}
	return findGoalStatus(root)
}

func findGoalStatus(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if goal, ok := typed["goal"].(map[string]any); ok {
			if rawStatus, ok := goal["status"].(string); ok {
				status := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rawStatus), "-", "_"))
				return status, status != ""
			}
		}
		for _, child := range typed {
			if status, ok := findGoalStatus(child); ok {
				return status, true
			}
		}
	case []any:
		for _, child := range typed {
			if status, ok := findGoalStatus(child); ok {
				return status, true
			}
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				return findGoalStatus(nested)
			}
		}
	}
	return "", false
}

func (a *App) resolveRole(input HookInput) resolvedRole {
	if session, pane, err := a.Tmux.CurrentSession(); err == nil {
		if session == a.SolSession {
			return resolvedRole{Kind: roleSol, Session: session, Pane: pane}
		}
		if a.IsWorkerSession(session) {
			return resolvedRole{Kind: roleWorker, Session: session, Pane: pane}
		}
	}
	st, err := a.Store.Read()
	if err != nil {
		return resolvedRole{}
	}
	if input.SessionID != "" && input.SessionID == st.Sol.CodexSessionID {
		return resolvedRole{Kind: roleSol, Session: st.Sol.Session, Pane: st.Sol.Pane}
	}
	for session, worker := range st.Workers {
		if input.SessionID != "" && input.SessionID == worker.CodexSessionID {
			return resolvedRole{Kind: roleWorker, Session: session, Pane: worker.Pane}
		}
	}
	for _, task := range state.RunningTasks(&st) {
		if pathContains(task.Workspace, input.CWD) {
			return resolvedRole{Kind: roleWorker, Session: task.WorkerSession, Pane: task.WorkerPane}
		}
	}
	if pathContains(st.Sol.CWD, input.CWD) && st.Sol.CWD != "" {
		return resolvedRole{Kind: roleSol, Session: st.Sol.Session, Pane: st.Sol.Pane}
	}
	return resolvedRole{}
}

func pathContains(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	root, _ = filepath.Abs(root)
	candidate, _ = filepath.Abs(candidate)
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (a *App) handleSessionStart(role resolvedRole, input HookInput) error {
	now := a.Now().UTC()
	return a.Store.Update(func(st *state.State) error {
		switch role.Kind {
		case roleSol:
			st.Sol.Session = role.Session
			st.Sol.Pane = role.Pane
			st.Sol.CodexSessionID = input.SessionID
			st.Sol.CWD = input.CWD
			st.Sol.Busy = false
			st.Sol.UpdatedAt = now
		case roleWorker:
			worker := st.Workers[role.Session]
			if worker == nil {
				worker = &state.Worker{Session: role.Session}
				st.Workers[role.Session] = worker
			}
			worker.Pane = role.Pane
			worker.CodexSessionID = input.SessionID
			worker.CWD = input.CWD
			worker.Busy = false
			worker.UpdatedAt = now
		}
		return nil
	})
}

func (a *App) handlePromptSubmit(role resolvedRole, input HookInput) error {
	now := a.Now().UTC()
	return a.Store.Update(func(st *state.State) error {
		switch role.Kind {
		case roleSol:
			st.Sol.Session = role.Session
			st.Sol.Pane = role.Pane
			st.Sol.CodexSessionID = input.SessionID
			st.Sol.CWD = input.CWD
			st.Sol.Busy = true
			st.Sol.TurnID = input.TurnID
			st.Sol.UpdatedAt = now
		case roleWorker:
			worker := st.Workers[role.Session]
			if worker == nil {
				worker = &state.Worker{Session: role.Session}
				st.Workers[role.Session] = worker
			}
			worker.Pane = role.Pane
			worker.CodexSessionID = input.SessionID
			worker.CWD = input.CWD
			worker.Busy = true
			worker.UpdatedAt = now
			if task := state.ActiveTaskForWorker(st, role.Session); task != nil {
				task.CodexSessionID = input.SessionID
				// Any later worker turn invalidates an implicit-completion check
				// scheduled by the preceding Stop.
				task.ReconcileToken = ""
				task.UpdatedAt = now
			}
		}
		return nil
	})
}

func (a *App) handleSolStop(role resolvedRole, input HookInput) error {
	var deliveryID string
	now := a.Now().UTC()
	if err := a.Store.Update(func(st *state.State) error {
		if st.Sol.ReservedDelivery != "" {
			if reserved := st.Deliveries[st.Sol.ReservedDelivery]; reserved != nil && reserved.Status == state.DeliverySending {
				reserved.Status = state.DeliveryPending
				reserved.LastError = "delivery reservation superseded by a later Sol stop"
			}
		}
		st.Sol.Session = role.Session
		st.Sol.Pane = role.Pane
		st.Sol.CodexSessionID = input.SessionID
		st.Sol.CWD = input.CWD
		st.Sol.Busy = false
		st.Sol.TurnID = ""
		st.Sol.ReservedDelivery = ""
		st.Sol.UpdatedAt = now
		if delivery := reserveOldestDelivery(st, now); delivery != nil {
			deliveryID = delivery.ID
		}
		return nil
	}); err != nil {
		return err
	}
	if deliveryID != "" {
		if err := a.SpawnDelivery(deliveryID, time.Duration(a.Config.DeliveryDelayMS)*time.Millisecond); err != nil {
			a.releaseDelivery(deliveryID, err)
			return err
		}
	}
	return nil
}

func (a *App) handleWorkerStop(role resolvedRole, input HookInput) error {
	now := a.Now().UTC()
	st, err := a.Store.Read()
	if err != nil {
		return err
	}
	task := state.ActiveTaskForWorker(&st, role.Session)
	if task == nil {
		return a.Store.Update(func(st *state.State) error {
			if worker := st.Workers[role.Session]; worker != nil {
				worker.Busy = false
				worker.UpdatedAt = now
			}
			return nil
		})
	}

	message := ""
	if input.LastAssistantMessage != nil {
		message = *input.LastAssistantMessage
	}
	cachePath := filepath.Join(a.Paths.CacheDir, task.ID+"-last-assistant.md")
	messageCached := false
	if message != "" {
		if err := writePrivate(cachePath, []byte(message)); err != nil {
			a.Logf("cache worker message for %s: %v", task.ID, err)
		} else {
			messageCached = true
		}
	}

	transcriptPath := ""
	if input.TranscriptPath != nil {
		transcriptPath = *input.TranscriptPath
	}
	goalEvent := transcript.GoalEvent{}
	found := false
	pendingTerminal := a.isTerminalGoalStatus(task.PendingGoalStatus)
	pendingTurnMatches := task.PendingGoalTurnID == "" || input.TurnID == "" || task.PendingGoalTurnID == input.TurnID
	pendingTurnMismatch := pendingTerminal && !pendingTurnMatches
	if pendingTerminal && pendingTurnMatches {
		goalEvent = transcript.GoalEvent{
			Status:    task.PendingGoalStatus,
			ThreadID:  input.SessionID,
			Timestamp: task.PendingGoalAt,
			Source:    "post-tool-use",
		}
		found = true
	} else if !pendingTurnMismatch {
		var lookupErr error
		goalEvent, found, _, lookupErr = a.lookupPersistedGoal(task, input.SessionID, transcriptPath)
		if lookupErr != nil {
			a.Logf("initial goal reconciliation for %s: %v", task.ID, lookupErr)
		}
	} else {
		a.Logf("ignore terminal goal status for %s: update_goal turn %q does not match Stop turn %q", task.ID, task.PendingGoalTurnID, input.TurnID)
	}

	if found && a.isTerminalGoalStatus(goalEvent.Status) {
		finalMessage := message
		if finalMessage == "" {
			finalMessage = "(Luna produced no final assistant message.)"
		}
		observedAt := goalEvent.Timestamp
		if observedAt.IsZero() {
			observedAt = now
		}
		_, err := a.finishTask(task.ID, finalMessage, goalEvent.Status, false, func(st *state.State, current *state.Task) error {
			current.CodexSessionID = input.SessionID
			current.TranscriptPath = transcriptPath
			if messageCached {
				current.LastAssistantPath = cachePath
			}
			current.ObservedGoalStatus = goalEvent.Status
			current.GoalObservedAt = observedAt
			current.PendingGoalStatus = ""
			current.PendingGoalTurnID = ""
			current.PendingGoalAt = time.Time{}
			current.LastStopTurnID = input.TurnID
			current.LastStopAt = now
			return nil
		})
		return err
	}

	reconcileCandidate := newID("rec", now)
	reconcileToken := ""
	if err := a.Store.Update(func(st *state.State) error {
		current := st.Tasks[task.ID]
		if current != nil && current.Status == state.TaskRunning {
			current.CodexSessionID = input.SessionID
			current.TranscriptPath = transcriptPath
			if messageCached {
				current.LastAssistantPath = cachePath
			}
			current.LastStopTurnID = input.TurnID
			current.LastStopAt = now
			if pendingTurnMismatch {
				current.PendingGoalStatus = ""
				current.PendingGoalTurnID = ""
				current.PendingGoalAt = time.Time{}
			}
			if found {
				observedAt := goalEvent.Timestamp
				if observedAt.IsZero() {
					observedAt = now
				}
				current.ObservedGoalStatus = goalEvent.Status
				current.GoalObservedAt = observedAt
				current.ReconcileToken = ""
				current.LastError = ""
			} else if !pendingTurnMismatch && current.ObservedGoalStatus == "" && messageCached && strings.TrimSpace(message) != "" {
				// The goal may have been submitted as a normal prompt. Schedule one
				// local re-check; a subsequent worker turn or any observed goal
				// invalidates this token before it can infer completion.
				current.ReconcileToken = reconcileCandidate
				current.LastError = ""
				reconcileToken = reconcileCandidate
			} else {
				current.ReconcileToken = ""
			}
			current.UpdatedAt = now
		}
		worker := st.Workers[role.Session]
		if worker == nil {
			worker = &state.Worker{Session: role.Session}
			st.Workers[role.Session] = worker
		}
		worker.Pane = role.Pane
		worker.CodexSessionID = input.SessionID
		worker.CWD = input.CWD
		worker.Busy = false
		worker.UpdatedAt = now
		return nil
	}); err != nil {
		return err
	}

	if reconcileToken != "" {
		delay := time.Duration(a.Config.GoalReconcileDelayMS) * time.Millisecond
		return a.ReconcileTask(task.ID, reconcileToken, delay)
	}
	return nil
}

func (a *App) isTerminalGoalStatus(status string) bool {
	status = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(status), "-", "_"))
	for _, allowed := range a.Config.TerminalGoalStatuses {
		if status == strings.ToLower(strings.ReplaceAll(strings.TrimSpace(allowed), "-", "_")) {
			return true
		}
	}
	return false
}

func reserveOldestDelivery(st *state.State, now time.Time) *state.Delivery {
	if st.Sol.Busy || st.Sol.ReservedDelivery != "" {
		return nil
	}
	pending := state.PendingDeliveries(st)
	if len(pending) == 0 {
		return nil
	}
	return reserveDelivery(st, pending[0].ID, now)
}

func reserveDelivery(st *state.State, id string, now time.Time) *state.Delivery {
	delivery := st.Deliveries[id]
	if delivery == nil || delivery.Status != state.DeliveryPending {
		return nil
	}
	delivery.Status = state.DeliverySending
	delivery.ReservedAt = now
	delivery.Attempts++
	delivery.LastError = ""
	st.Sol.Busy = true
	st.Sol.ReservedDelivery = id
	st.Sol.UpdatedAt = now
	return delivery
}
