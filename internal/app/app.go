package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/project"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
)

type App struct {
	ProjectID    string
	BasePaths    config.Paths
	Paths        config.Paths
	Config       config.Config
	BrainSession string
	Store        *state.Store
	Tmux         tmux.Client
	Executable   string
	Now          func() time.Time
	workerRE     *regexp.Regexp
}

func New() (*App, error) {
	return NewForProject("")
}

// NewForProject constructs a project-scoped Conductor instance. An empty
// project name is inferred from the current tmux session, then from
// CONDUCTOR_PROJECT, and finally falls back to the default project.
func NewForProject(requestedProject string) (*App, error) {
	basePaths, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	if err := config.EnsureDirectories(basePaths); err != nil {
		return nil, err
	}
	cfg, err := config.Load(basePaths)
	if err != nil {
		return nil, err
	}
	tmuxClient := tmux.NewWithGoalDispatchOptions(cfg.TmuxCommand, tmux.GoalDispatchOptions{
		PrefixDelay:         time.Duration(cfg.GoalPrefixDelayMS) * time.Millisecond,
		ReplaceProbeTimeout: time.Duration(cfg.GoalReplaceProbeMS) * time.Millisecond,
	})
	projectID := strings.TrimSpace(requestedProject)
	if projectID == "" {
		if session, _, currentErr := tmuxClient.CurrentSession(); currentErr == nil {
			if parsed, ok := project.ParseSession(session, cfg.BrainSession, cfg.WorkerSessionPattern); ok {
				projectID = parsed.ProjectID
			}
		}
	}
	if projectID == "" {
		projectID = os.Getenv("CONDUCTOR_PROJECT")
	}
	if projectID == "" {
		projectID = project.DefaultID
	}
	projectID, err = project.NormalizeID(projectID)
	if err != nil {
		return nil, err
	}
	paths := config.ForProject(basePaths, projectID)
	if err := config.EnsureDirectories(paths); err != nil {
		return nil, err
	}
	brainSession := project.BrainSession(projectID, cfg.BrainSession)
	workerPattern := project.WorkerPattern(projectID, cfg.WorkerSessionPattern)
	workerRE, err := regexp.Compile(workerPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid worker_session_pattern: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve conductor executable: %w", err)
	}
	executable, _ = filepath.Abs(executable)
	store := state.NewProjectStore(paths, projectID, brainSession)
	return &App{
		ProjectID:    projectID,
		BasePaths:    basePaths,
		Paths:        paths,
		Config:       cfg,
		BrainSession: brainSession,
		Store:        store,
		Tmux:         tmuxClient,
		Executable:   executable,
		Now:          time.Now,
		workerRE:     workerRE,
	}, nil
}

func (a *App) Init() error {
	if _, err := os.Stat(a.Paths.Config); errors.Is(err, os.ErrNotExist) {
		if err := config.Save(a.Paths, a.Config); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return a.Store.Init()
}

// SendBrainSetup serializes setup prompt validation and submission with every
// state-coordinated Brain delivery. The callback must perform its final live
// pane checks; it runs while the project state lock prevents a handoff lease
// from being claimed concurrently.
func (a *App) SendBrainSetup(prompt string, validate func(tmux.Pane) error) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("setup prompt cannot be empty")
	}
	return a.Store.Update(func(st *state.State) error {
		if st.Brain.ReservedDelivery != "" {
			return errors.New("Brain has a reserved handoff; setup prompt was not sent")
		}
		if st.Brain.Busy || st.Brain.TurnID != "" {
			return errors.New("Brain is working; setup prompt was not sent")
		}
		pane, err := a.Tmux.ResolvePane(a.BrainSession)
		if err != nil {
			return err
		}
		if validate != nil {
			if err := validate(pane); err != nil {
				return err
			}
		}
		if err := a.Tmux.SendPrompt(pane.ID, prompt); err != nil {
			return err
		}
		now := a.Now().UTC()
		st.Brain.Session = a.BrainSession
		st.Brain.Pane = pane.ID
		st.Brain.Busy = true
		st.Brain.UpdatedAt = now
		return nil
	})
}

func (a *App) IsWorkerSession(session string) bool {
	if a.ProjectID == project.DefaultID && strings.Contains(session, project.SessionSeparator) {
		return false
	}
	return a.workerRE.MatchString(session)
}

func (a *App) ResolveWorkerSession(worker string) (physical string, alias string, err error) {
	return project.WorkerSession(a.ProjectID, worker, a.Config.WorkerSessionPattern)
}

func (a *App) Delegate(worker, objective string) (*state.Task, error) {
	workerSession, workerAlias, err := a.ResolveWorkerSession(worker)
	if err != nil {
		return nil, err
	}
	if !a.IsWorkerSession(workerSession) {
		return nil, fmt.Errorf("worker %q is not valid for project %q", workerSession, a.ProjectID)
	}
	if strings.TrimSpace(objective) == "" {
		return nil, errors.New("goal objective cannot be empty")
	}
	pane, err := a.Tmux.ResolvePane(workerSession)
	if err != nil {
		return nil, err
	}
	senderSession, senderPane, _ := a.Tmux.CurrentSession()
	if senderSession == "" {
		senderSession = a.BrainSession
	}

	now := a.Now().UTC()
	taskID := newID("tsk", now)
	taskDir := filepath.Join(a.Paths.TasksDir, taskID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return nil, err
	}
	objectivePath := filepath.Join(taskDir, "goal.md")
	if err := writePrivate(objectivePath, []byte(objective)); err != nil {
		return nil, fmt.Errorf("write goal file: %w", err)
	}

	goalObjective := objective
	if utf8.RuneCountInString(goalObjective) > a.Config.InlineGoalMaxChars {
		goalObjective = fmt.Sprintf("Read and execute the complete goal in `%s`.", objectivePath)
	}

	task := &state.Task{
		ID:                taskID,
		WorkerSession:     workerSession,
		WorkerAlias:       workerAlias,
		WorkerPane:        pane.ID,
		Workspace:         pane.Path,
		SenderSession:     senderSession,
		Status:            state.TaskRunning,
		ObjectivePath:     objectivePath,
		SentGoalObjective: goalObjective,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := a.Store.Update(func(st *state.State) error {
		if active := state.ActiveTaskForWorker(st, workerSession); active != nil {
			return fmt.Errorf("%s already has active task %s", workerSession, active.ID)
		}
		st.Tasks[task.ID] = task
		st.Workers[workerSession] = &state.Worker{Session: workerSession, Pane: pane.ID, CWD: pane.Path, Busy: true, UpdatedAt: now}
		if senderSession == a.BrainSession {
			st.Brain.Session = senderSession
			st.Brain.Pane = senderPane
			st.Brain.Busy = true
			st.Brain.UpdatedAt = now
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := a.Tmux.SendGoal(pane.ID, goalObjective); err != nil {
		_ = a.Store.Update(func(st *state.State) error {
			if current := st.Tasks[task.ID]; current != nil {
				current.Status = state.TaskFailed
				current.LastError = err.Error()
				current.UpdatedAt = a.Now().UTC()
				current.FinishedAt = current.UpdatedAt
			}
			if workerState := st.Workers[workerSession]; workerState != nil {
				workerState.Busy = false
				workerState.UpdatedAt = a.Now().UTC()
			}
			return nil
		})
		return nil, err
	}
	copyTask := *task
	return &copyTask, nil
}

func newID(prefix string, now time.Time) string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%s-%d", prefix, now.Format("20060102-150405"), os.Getpid())
	}
	return fmt.Sprintf("%s-%s-%s", prefix, now.Format("20060102-150405"), hex.EncodeToString(buf))
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (a *App) StatusJSON() ([]byte, error) {
	st, err := a.Store.Read()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(st, "", "  ")
}

func (a *App) StatusText() (string, error) {
	st, err := a.Store.Read()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", a.ProjectID)
	brainState := "idle"
	if st.Brain.Busy {
		brainState = "busy"
	}
	fmt.Fprintf(&b, "Brain: %s (%s)", st.Brain.Session, brainState)
	if st.Brain.Pane != "" {
		fmt.Fprintf(&b, " pane=%s", st.Brain.Pane)
	}
	b.WriteByte('\n')

	tasks := make([]*state.Task, 0, len(st.Tasks))
	for _, task := range st.Tasks {
		copyTask := *task
		tasks = append(tasks, &copyTask)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	if len(tasks) == 0 {
		b.WriteString("Tasks: none\n")
	} else {
		b.WriteString("Tasks:\n")
		for index, task := range tasks {
			worker := task.WorkerAlias
			if worker == "" {
				worker = task.WorkerSession
			}
			fmt.Fprintf(&b, "  %-28s %-8s %-10s %s", task.ID, worker, task.Status, task.Workspace)
			if task.TerminalGoalStatus != "" {
				fmt.Fprintf(&b, " goal=%s", task.TerminalGoalStatus)
			}
			if task.LastError != "" {
				fmt.Fprintf(&b, " error=%q", task.LastError)
			}
			b.WriteByte('\n')
			if index == 19 && len(tasks) > 20 {
				fmt.Fprintf(&b, "  … %d older tasks omitted\n", len(tasks)-20)
				break
			}
		}
	}
	pending := state.PendingDeliveries(&st)
	fmt.Fprintf(&b, "Pending handoffs: %d\n", len(pending))
	for _, delivery := range pending {
		worker := delivery.WorkerAlias
		if worker == "" {
			worker = delivery.WorkerSession
		}
		fmt.Fprintf(&b, "  %s from %s task=%s\n", delivery.ID, worker, delivery.TaskID)
	}
	return b.String(), nil
}
