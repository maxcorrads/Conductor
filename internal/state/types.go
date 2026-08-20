package state

import "time"

const CurrentVersion = 1

const (
	TaskRunning   = "running"
	TaskFinished  = "finished"
	TaskCancelled = "cancelled"
	TaskFailed    = "failed"

	DeliveryPending   = "pending"
	DeliverySending   = "sending"
	DeliveryDelivered = "delivered"
)

type Activity struct {
	Session          string    `json:"session"`
	Pane             string    `json:"pane,omitempty"`
	CodexSessionID   string    `json:"codex_session_id,omitempty"`
	CWD              string    `json:"cwd,omitempty"`
	Busy             bool      `json:"busy"`
	TurnID           string    `json:"turn_id,omitempty"`
	ReservedDelivery string    `json:"reserved_delivery,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type Worker struct {
	Session        string    `json:"session"`
	Pane           string    `json:"pane,omitempty"`
	CodexSessionID string    `json:"codex_session_id,omitempty"`
	CWD            string    `json:"cwd,omitempty"`
	Busy           bool      `json:"busy"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type Task struct {
	ID                 string    `json:"id"`
	WorkerSession      string    `json:"worker_session"`
	WorkerAlias        string    `json:"worker_alias,omitempty"`
	WorkerPane         string    `json:"worker_pane,omitempty"`
	Workspace          string    `json:"workspace"`
	SenderSession      string    `json:"sender_session,omitempty"`
	Status             string    `json:"status"`
	TerminalGoalStatus string    `json:"terminal_goal_status,omitempty"`
	ObjectivePath      string    `json:"objective_path"`
	SentGoalObjective  string    `json:"sent_goal_objective"`
	CodexSessionID     string    `json:"codex_session_id,omitempty"`
	TranscriptPath     string    `json:"transcript_path,omitempty"`
	LastAssistantPath  string    `json:"last_assistant_path,omitempty"`
	PendingGoalStatus  string    `json:"pending_goal_status,omitempty"`
	PendingGoalTurnID  string    `json:"pending_goal_turn_id,omitempty"`
	PendingGoalAt      time.Time `json:"pending_goal_at,omitempty"`
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
	Sol        Activity             `json:"sol"`
	Workers    map[string]*Worker   `json:"workers"`
	Tasks      map[string]*Task     `json:"tasks"`
	Deliveries map[string]*Delivery `json:"deliveries"`
}

func New(solSession string) State {
	return NewForProject("default", solSession)
}

func NewForProject(projectID, solSession string) State {
	if projectID == "" {
		projectID = "default"
	}
	return State{
		Version:    CurrentVersion,
		ProjectID:  projectID,
		Sol:        Activity{Session: solSession},
		Workers:    map[string]*Worker{},
		Tasks:      map[string]*Task{},
		Deliveries: map[string]*Delivery{},
	}
}
