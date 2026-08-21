package app

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/transcript"
)

var errReconciliationCancelled = errors.New("reconciliation cancelled")

// ReconcileTask performs exactly one delayed local check for a worker Stop that
// did not expose a persisted goal. A subsequent worker turn, any observed goal,
// or another Stop invalidates the token and cancels the inference.
func (a *App) ReconcileTask(taskID, token string, delay time.Duration) error {
	if delay > 0 {
		time.Sleep(delay)
	}
	st, err := a.Store.Read()
	if err != nil {
		return err
	}
	task := st.Tasks[taskID]
	if task == nil || task.Status != state.TaskRunning || task.ReconcileToken != token {
		return nil
	}
	if worker := st.Workers[task.WorkerSession]; worker != nil && worker.Busy {
		return a.clearReconciliation(taskID, token)
	}

	goalEvent, found, checked, lookupErr := a.lookupPersistedGoal(task, task.CodexSessionID, task.TranscriptPath)
	if lookupErr != nil {
		return a.cancelReconciliation(taskID, token, "reconcile goal state: "+lookupErr.Error())
	}
	if found {
		observedAt := goalEvent.Timestamp
		if observedAt.IsZero() {
			observedAt = a.Now().UTC()
		}
		if !a.isTerminalGoalStatus(goalEvent.Status) {
			return a.Store.Update(func(st *state.State) error {
				current := st.Tasks[taskID]
				if current == nil || current.Status != state.TaskRunning || current.ReconcileToken != token {
					return nil
				}
				current.ObservedGoalStatus = goalEvent.Status
				current.GoalObservedAt = observedAt
				current.ReconcileToken = ""
				current.LastError = ""
				current.UpdatedAt = a.Now().UTC()
				return nil
			})
		}

		message := cachedAssistantMessage(task)
		if strings.TrimSpace(message) == "" {
			message = "(Luna produced no final assistant message.)"
		}
		_, err := a.finishTask(taskID, message, goalEvent.Status, false, func(st *state.State, current *state.Task) error {
			if err := reconciliationGuard(st, current, token); err != nil {
				return err
			}
			current.ObservedGoalStatus = goalEvent.Status
			current.GoalObservedAt = observedAt
			current.PendingGoalStatus = ""
			current.PendingGoalTurnID = ""
			current.PendingGoalAt = time.Time{}
			return nil
		})
		if errors.Is(err, errReconciliationCancelled) || errors.Is(err, errTaskNotRunning) {
			return nil
		}
		return err
	}

	if !checked {
		return a.cancelReconciliation(taskID, token, "implicit reconciliation skipped: no readable Codex goal source")
	}

	message := cachedAssistantMessage(task)
	if strings.TrimSpace(message) == "" {
		// There is no reliable handoff to forward. Clear only this still-current
		// attempt and leave the task available for manual recovery.
		return a.Store.Update(func(st *state.State) error {
			current := st.Tasks[taskID]
			if current != nil && current.Status == state.TaskRunning && current.ReconcileToken == token {
				current.ReconcileToken = ""
				current.UpdatedAt = a.Now().UTC()
			}
			return nil
		})
	}

	_, err = a.finishTask(taskID, message, "implicit", false, func(st *state.State, current *state.Task) error {
		if err := reconciliationGuard(st, current, token); err != nil {
			return err
		}
		if current.ObservedGoalStatus != "" {
			return errReconciliationCancelled
		}
		return nil
	})
	if errors.Is(err, errReconciliationCancelled) || errors.Is(err, errTaskNotRunning) {
		return nil
	}
	return err
}

func reconciliationGuard(st *state.State, task *state.Task, token string) error {
	if task.ReconcileToken != token {
		return errReconciliationCancelled
	}
	if worker := st.Workers[task.WorkerSession]; worker != nil && worker.Busy {
		return errReconciliationCancelled
	}
	return nil
}

func cachedAssistantMessage(task *state.Task) string {
	if task == nil || task.LastAssistantPath == "" {
		return ""
	}
	data, err := os.ReadFile(task.LastAssistantPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// lookupPersistedGoal checks only local Codex artifacts. Transcript JSONL is
// preferred and SQLite is a compatibility fallback; both are matched against
// the active task objective to avoid adopting a stale goal from another task.
func (a *App) lookupPersistedGoal(task *state.Task, sessionID, transcriptPath string) (transcript.GoalEvent, bool, bool, error) {
	if task == nil {
		return transcript.GoalEvent{}, false, false, nil
	}
	if sessionID == "" {
		sessionID = task.CodexSessionID
	}
	if transcriptPath == "" {
		transcriptPath = task.TranscriptPath
	}
	var lookupErrors []error
	checked := false
	if transcriptPath != "" {
		checked = true
		goalEvent, found, err := transcript.LatestGoalEvent(
			transcriptPath,
			sessionID,
			task.SentGoalObjective,
			task.CreatedAt,
			a.Config.TranscriptTailBytes,
		)
		if err != nil {
			a.Logf("parse goal status for %s: %v", task.ID, err)
			lookupErrors = append(lookupErrors, err)
		}
		if found {
			return goalEvent, true, true, nil
		}
	}
	dbEvent, dbFound, dbChecked, dbErr := transcript.GoalStatusFromSQLiteChecked(sessionID)
	checked = checked || dbChecked
	if dbErr != nil {
		a.Logf("read goal DB for %s: %v", task.ID, dbErr)
		lookupErrors = append(lookupErrors, dbErr)
	} else if dbFound && (dbEvent.Objective == "" || transcript.ObjectivesMatch(dbEvent.Objective, task.SentGoalObjective)) {
		return dbEvent, true, true, nil
	} else if dbFound {
		a.Logf("ignore goal DB status for %s: objective does not match active task", task.ID)
	}
	return transcript.GoalEvent{}, false, checked, errors.Join(lookupErrors...)
}

func (a *App) clearReconciliation(taskID, token string) error {
	return a.Store.Update(func(st *state.State) error {
		current := st.Tasks[taskID]
		if current != nil && current.Status == state.TaskRunning && current.ReconcileToken == token {
			current.ReconcileToken = ""
			current.UpdatedAt = a.Now().UTC()
		}
		return nil
	})
}

func (a *App) cancelReconciliation(taskID, token, reason string) error {
	return a.Store.Update(func(st *state.State) error {
		current := st.Tasks[taskID]
		if current != nil && current.Status == state.TaskRunning && current.ReconcileToken == token {
			current.ReconcileToken = ""
			current.LastError = reason
			current.UpdatedAt = a.Now().UTC()
		}
		return nil
	})
}
