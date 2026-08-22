package state

import "time"

const CurrentVersion = 2

const (
	TaskRunning   = "running"
	TaskFinished  = "finished"
	TaskCancelled = "cancelled"
	TaskFailed    = "failed"

	DeliveryPending   = "pending"
	DeliverySending   = "sending"
	DeliveryPasting   = "pasting"
	DeliveryDelivered = "delivered"
)

type Activity struct {
	Session                string    `json:"session"`
	Pane                   string    `json:"pane,omitempty"`
	CodexSessionID         string    `json:"codex_session_id,omitempty"`
	CodexSessionObservedAt time.Time `json:"codex_session_observed_at,omitempty"`
	TmuxSessionCreatedAt   time.Time `json:"tmux_session_created_at,omitempty"`
	CWD                    string    `json:"cwd,omitempty"`
	Busy                   bool      `json:"busy"`
	TurnID                 string    `json:"turn_id,omitempty"`
	ReservedDelivery       string    `json:"reserved_delivery,omitempty"`
	UpdatedAt              time.Time `json:"updated_at,omitempty"`
}

type Worker struct {
	Session                string    `json:"session"`
	Pane                   string    `json:"pane,omitempty"`
	CodexSessionID         string    `json:"codex_session_id,omitempty"`
	CodexSessionObservedAt time.Time `json:"codex_session_observed_at,omitempty"`
	TmuxSessionCreatedAt   time.Time `json:"tmux_session_created_at,omitempty"`
	CWD                    string    `json:"cwd,omitempty"`
	Busy                   bool      `json:"busy"`
	UpdatedAt              time.Time `json:"updated_at,omitempty"`
}

type Task struct {
	ID                 string    `json:"id"`
	WorkerSession      string    `json:"worker_session"`
	WorkerAlias        string    `json:"worker_alias,omitempty"`
	WorkerPane         string    `json:"worker_pane,omitempty"`
	Workspace          string    `json:"workspace"`
	SenderSession      string    `json:"sender_session,omitempty"`
	Status             string    `json:"status"`
	DispatchState      string    `json:"dispatch_state,omitempty"`
	TerminalGoalStatus string    `json:"terminal_goal_status,omitempty"`
	ObjectivePath      string    `json:"objective_path"`
	SentGoalObjective  string    `json:"sent_goal_objective"`
	CodexSessionID     string    `json:"codex_session_id,omitempty"`
	TranscriptPath     string    `json:"transcript_path,omitempty"`
	LastAssistantPath  string    `json:"last_assistant_path,omitempty"`
	PendingGoalStatus  string    `json:"pending_goal_status,omitempty"`
	PendingGoalTurnID  string    `json:"pending_goal_turn_id,omitempty"`
	PendingGoalAt      time.Time `json:"pending_goal_at,omitempty"`
	ObservedGoalStatus string    `json:"observed_goal_status,omitempty"`
	GoalObservedAt     time.Time `json:"goal_observed_at,omitempty"`
	ReconcileToken     string    `json:"reconcile_token,omitempty"`
	LastStopTurnID     string    `json:"last_stop_turn_id,omitempty"`
	LastStopAt         time.Time `json:"last_stop_at,omitempty"`
	DeliveryID         string    `json:"delivery_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	FinishedAt         time.Time `json:"finished_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
}

type Delivery struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	WorkerSession string    `json:"worker_session"`
	WorkerAlias   string    `json:"worker_alias,omitempty"`
	Workspace     string    `json:"workspace"`
	GoalStatus    string    `json:"goal_status"`
	MessagePath   string    `json:"message_path"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ReservedAt    time.Time `json:"reserved_at,omitempty"`
	DeliveredAt   time.Time `json:"delivered_at,omitempty"`
}

type State struct {
	Version    int                  `json:"version"`
	ProjectID  string               `json:"project_id,omitempty"`
	Brain      Activity             `json:"brain"`
	Workers    map[string]*Worker   `json:"workers"`
	Tasks      map[string]*Task     `json:"tasks"`
	Deliveries map[string]*Delivery `json:"deliveries"`
}

func New(brainSession string) State {
	return NewForProject("default", brainSession)
}

func NewForProject(projectID, brainSession string) State {
	if projectID == "" {
		projectID = "default"
	}
	return State{
		Version:    CurrentVersion,
		ProjectID:  projectID,
		Brain:      Activity{Session: brainSession},
		Workers:    map[string]*Worker{},
		Tasks:      map[string]*Task{},
		Deliveries: map[string]*Delivery{},
	}
}
