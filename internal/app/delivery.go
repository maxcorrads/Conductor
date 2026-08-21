package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/maxcorrads/conductor/internal/state"
)

func (a *App) SpawnDelivery(deliveryID string, delay time.Duration) error {
	logFile, err := os.OpenFile(a.Paths.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(a.Executable, "_deliver", deliveryID, "--project", a.ProjectID, "--delay-ms", strconv.FormatInt(delay.Milliseconds(), 10))
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start delivery helper: %w", err)
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()
	return nil
}

func (a *App) Deliver(deliveryID string, delay time.Duration) error {
	if delay > 0 {
		time.Sleep(delay)
	}
	st, err := a.Store.Read()
	if err != nil {
		return err
	}
	delivery := st.Deliveries[deliveryID]
	if delivery == nil {
		return fmt.Errorf("unknown delivery %s", deliveryID)
	}
	if delivery.Status == state.DeliveryDelivered {
		return nil
	}
	if delivery.Status != state.DeliverySending {
		return fmt.Errorf("delivery %s is %s, not sending", deliveryID, delivery.Status)
	}
	if st.Sol.ReservedDelivery != deliveryID {
		return fmt.Errorf("delivery %s is no longer reserved for Sol", deliveryID)
	}
	if st.Sol.TurnID != "" {
		if err := a.requeueDelivery(deliveryID, "Sol started another turn before the handoff could be pasted"); err != nil {
			return err
		}
		return nil
	}
	raw, err := os.ReadFile(delivery.MessagePath)
	if err != nil {
		a.releaseDelivery(deliveryID, err)
		return err
	}
	pane, err := a.Tmux.ResolvePane(a.SolSession)
	if err != nil {
		a.releaseDelivery(deliveryID, err)
		return err
	}
	paneID := pane.ID
	prompt := formatHandoff(delivery, string(raw))
	if err := a.Tmux.SendPrompt(paneID, prompt); err != nil {
		// The stored pane may have changed after a tmux restart; retry using the active pane.
		pane, resolveErr := a.Tmux.ResolvePane(a.SolSession)
		if resolveErr == nil && pane.ID != paneID {
			if retryErr := a.Tmux.SendPrompt(pane.ID, prompt); retryErr == nil {
				paneID = pane.ID
				err = nil
			}
		}
		if err != nil {
			a.releaseDelivery(deliveryID, err)
			return err
		}
	}
	now := a.Now().UTC()
	return a.Store.Update(func(st *state.State) error {
		d := st.Deliveries[deliveryID]
		if d == nil {
			return nil
		}
		d.Status = state.DeliveryDelivered
		d.DeliveredAt = now
		d.LastError = ""
		if st.Sol.ReservedDelivery == deliveryID {
			st.Sol.ReservedDelivery = ""
		}
		st.Sol.Pane = paneID
		// Keep busy=true. UserPromptSubmit will confirm it, and Stop will clear it.
		st.Sol.Busy = true
		st.Sol.UpdatedAt = now
		return nil
	})
}

func formatHandoff(delivery *state.Delivery, raw string) string {
	worker := delivery.WorkerAlias
	if worker == "" {
		worker = delivery.WorkerSession
	}
	header := fmt.Sprintf("[CONDUCTOR HANDOFF | %s | %s]", worker, delivery.GoalStatus)
	workspace := "workspace: " + delivery.Workspace
	if raw == "" {
		raw = "(empty handoff)"
	}
	return header + "\n" + workspace + "\n\n" + raw
}

func (a *App) releaseDelivery(deliveryID string, cause error) {
	_ = a.Store.Update(func(st *state.State) error {
		d := st.Deliveries[deliveryID]
		if d != nil && d.Status != state.DeliveryDelivered {
			d.Status = state.DeliveryPending
			d.LastError = cause.Error()
		}
		if st.Sol.ReservedDelivery == deliveryID {
			st.Sol.ReservedDelivery = ""
			st.Sol.Busy = st.Sol.TurnID != ""
		}
		st.Sol.UpdatedAt = a.Now().UTC()
		return nil
	})
	a.Logf("delivery %s failed: %v", deliveryID, cause)
}

func (a *App) requeueDelivery(deliveryID, reason string) error {
	now := a.Now().UTC()
	return a.Store.Update(func(st *state.State) error {
		delivery := st.Deliveries[deliveryID]
		if delivery != nil && delivery.Status != state.DeliveryDelivered {
			delivery.Status = state.DeliveryPending
			delivery.LastError = reason
		}
		if st.Sol.ReservedDelivery == deliveryID {
			st.Sol.ReservedDelivery = ""
		}
		st.Sol.Busy = st.Sol.TurnID != ""
		st.Sol.UpdatedAt = now
		return nil
	})
}

func (a *App) FinishWorker(worker, explicitMessage, goalStatus string) (string, error) {
	workerSession, _, err := a.ResolveWorkerSession(worker)
	if err != nil {
		return "", err
	}
	if !a.IsWorkerSession(workerSession) {
		return "", fmt.Errorf("invalid worker %q", workerSession)
	}
	st, err := a.Store.Read()
	if err != nil {
		return "", err
	}
	task := state.ActiveTaskForWorker(&st, workerSession)
	if task == nil {
		return "", fmt.Errorf("%s has no active task", workerSession)
	}
	if goalStatus == "" {
		goalStatus = "manual"
	}
	return a.finishTask(task.ID, explicitMessage, goalStatus, true, nil)
}

var errTaskNotRunning = errors.New("task is no longer running")

type finishTaskMutation func(st *state.State, task *state.Task) error

// finishTask turns a running task into a queued handoff. Callers may provide a
// mutation that is evaluated under the state lock; reconciliation uses it to
// prove that the worker stayed idle and that no real goal appeared meanwhile.
func (a *App) finishTask(taskID, explicitMessage, goalStatus string, useCachedMessage bool, mutate finishTaskMutation) (string, error) {
	st, err := a.Store.Read()
	if err != nil {
		return "", err
	}
	task := st.Tasks[taskID]
	if task == nil || task.Status != state.TaskRunning {
		return "", errTaskNotRunning
	}

	message := explicitMessage
	if message == "" && useCachedMessage && task.LastAssistantPath != "" {
		if data, readErr := os.ReadFile(task.LastAssistantPath); readErr == nil {
			message = string(data)
		}
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("no cached assistant message; pass --stdin or --file")
	}
	if strings.TrimSpace(goalStatus) == "" {
		goalStatus = "manual"
	}

	now := a.Now().UTC()
	deliveryID := newID("msg", now)
	handoffPath := a.Paths.HandoffsDir + string(os.PathSeparator) + deliveryID + ".md"
	if err := writePrivate(handoffPath, []byte(message)); err != nil {
		return "", err
	}

	deliverNowID := ""
	if err := a.Store.Update(func(st *state.State) error {
		current := st.Tasks[taskID]
		if current == nil || current.Status != state.TaskRunning {
			return errTaskNotRunning
		}
		if mutate != nil {
			if err := mutate(st, current); err != nil {
				return err
			}
		}
		current.Status = state.TaskFinished
		current.TerminalGoalStatus = goalStatus
		current.LastError = ""
		current.ReconcileToken = ""
		current.DeliveryID = deliveryID
		current.FinishedAt = now
		current.UpdatedAt = now
		st.Deliveries[deliveryID] = &state.Delivery{
			ID: deliveryID, TaskID: current.ID, WorkerSession: current.WorkerSession, WorkerAlias: current.WorkerAlias,
			Workspace: current.Workspace, GoalStatus: goalStatus,
			MessagePath: handoffPath, Status: state.DeliveryPending, CreatedAt: now,
		}
		if workerState := st.Workers[current.WorkerSession]; workerState != nil {
			workerState.Busy = false
			workerState.UpdatedAt = now
		}
		if !st.Sol.Busy && st.Sol.ReservedDelivery == "" {
			if reserved := reserveOldestDelivery(st, now); reserved != nil {
				deliverNowID = reserved.ID
			}
		}
		return nil
	}); err != nil {
		_ = os.Remove(handoffPath)
		return "", err
	}
	if deliverNowID != "" {
		if err := a.Deliver(deliverNowID, time.Duration(a.Config.DeliveryDelayMS)*time.Millisecond); err != nil {
			return deliveryID, err
		}
	}
	return deliveryID, nil
}

func (a *App) Flush(force bool) (string, error) {
	var deliveryID string
	now := a.Now().UTC()
	if err := a.Store.Update(func(st *state.State) error {
		if force {
			st.Sol.Busy = false
			st.Sol.TurnID = ""
			st.Sol.ReservedDelivery = ""
		}
		if st.Sol.Busy {
			return errors.New("Sol is busy; handoff remains queued")
		}
		if delivery := reserveOldestDelivery(st, now); delivery != nil {
			deliveryID = delivery.ID
		}
		return nil
	}); err != nil {
		return "", err
	}
	if deliveryID == "" {
		return "", nil
	}
	return deliveryID, a.Deliver(deliveryID, 0)
}

func (a *App) MarkSolIdle() error {
	return a.Store.Update(func(st *state.State) error {
		if st.Sol.ReservedDelivery != "" {
			if d := st.Deliveries[st.Sol.ReservedDelivery]; d != nil && d.Status == state.DeliverySending {
				d.Status = state.DeliveryPending
				d.LastError = "reservation reset manually"
			}
		}
		st.Sol.ReservedDelivery = ""
		st.Sol.Busy = false
		st.Sol.TurnID = ""
		st.Sol.UpdatedAt = a.Now().UTC()
		return nil
	})
}
