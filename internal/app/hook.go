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
	if !ok || !a.isTerminalGoalStatus(status) {
		return nil
	}
	// PostToolUse also fires for failed local function calls. When Codex provides
	// a response, require the goal object in that response to confirm the status;
	// otherwise a failed update_goal attempt could end the worker prematurely.
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
		task.PendingGoalStatus = status
		task.PendingGoalTurnID = input.TurnID
		task.PendingGoalAt = now
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
	if message != "" {
		if err := writePrivate(cachePath, []byte(message)); err != nil {
			a.Logf("cache worker message for %s: %v", task.ID, err)
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
		var parseErr error
		goalEvent, found, parseErr = transcript.LatestGoalEvent(
			transcriptPath,
			input.SessionID,
			task.SentGoalObjective,
			task.CreatedAt,
			a.Config.TranscriptTailBytes,
		)
		if parseErr != nil {
			a.Logf("parse goal status for %s: %v", task.ID, parseErr)
		}
	} else {
		a.Logf("ignore terminal goal status for %s: update_goal turn %q does not match Stop turn %q", task.ID, task.PendingGoalTurnID, input.TurnID)
	}
	if !found && !pendingTurnMismatch {
		if dbEvent, dbFound, dbErr := transcript.GoalStatusFromSQLite(input.SessionID); dbErr != nil {
			a.Logf("read goal DB for %s: %v", task.ID, dbErr)
		} else if dbFound && (dbEvent.Objective == "" || transcript.ObjectivesMatch(dbEvent.Objective, task.SentGoalObjective)) {
			goalEvent, found = dbEvent, true
		} else if dbFound {
			a.Logf("ignore goal DB status for %s: objective does not match active task", task.ID)
		}
	}
	terminal := found && a.isTerminalGoalStatus(goalEvent.Status)

	var deliveryID string
	var deliverNowID string
	if terminal {
		deliveryID = newID("msg", now)
		handoffPath := filepath.Join(a.Paths.HandoffsDir, deliveryID+".md")
		if message == "" {
			message = "(Luna produced no final assistant message.)"
		}
		if err := writePrivate(handoffPath, []byte(message)); err != nil {
			return fmt.Errorf("write handoff: %w", err)
		}
		if err := a.Store.Update(func(st *state.State) error {
			current := st.Tasks[task.ID]
			if current == nil || current.Status != state.TaskRunning {
				deliveryID = ""
				return nil
			}
			current.Status = state.TaskFinished
			current.TerminalGoalStatus = goalEvent.Status
			current.CodexSessionID = input.SessionID
			current.TranscriptPath = transcriptPath
			if message != "" {
				current.LastAssistantPath = cachePath
			}
			current.DeliveryID = deliveryID
			current.UpdatedAt = now
			current.FinishedAt = now
			st.Deliveries[deliveryID] = &state.Delivery{
				ID: deliveryID, TaskID: current.ID, WorkerSession: current.WorkerSession, WorkerAlias: current.WorkerAlias,
				Workspace: current.Workspace, GoalStatus: goalEvent.Status,
				MessagePath: handoffPath, Status: state.DeliveryPending, CreatedAt: now,
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
			if !st.Sol.Busy && st.Sol.ReservedDelivery == "" {
				if reserved := reserveOldestDelivery(st, now); reserved != nil {
					deliverNowID = reserved.ID
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if deliveryID == "" {
			_ = os.Remove(handoffPath)
		}
		if deliverNowID != "" {
			if err := a.Deliver(deliverNowID, time.Duration(a.Config.DeliveryDelayMS)*time.Millisecond); err != nil {
				return err
			}
		}
		return nil
	}

	return a.Store.Update(func(st *state.State) error {
		current := st.Tasks[task.ID]
		if current != nil && current.Status == state.TaskRunning {
			current.CodexSessionID = input.SessionID
			current.TranscriptPath = transcriptPath
			if message != "" {
				current.LastAssistantPath = cachePath
			}
			if pendingTurnMismatch {
				current.PendingGoalStatus = ""
				current.PendingGoalTurnID = ""
				current.PendingGoalAt = time.Time{}
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
	})
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
