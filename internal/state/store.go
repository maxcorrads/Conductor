package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/maxcorrads/conductor/internal/config"
	"github.com/maxcorrads/conductor/internal/lock"
)

type Store struct {
	Paths      config.Paths
	ProjectID  string
	SolSession string
	Now        func() time.Time
}

func NewStore(paths config.Paths, solSession string) *Store {
	return NewProjectStore(paths, "default", solSession)
}

func NewProjectStore(paths config.Paths, projectID, solSession string) *Store {
	if projectID == "" {
		projectID = "default"
	}
	return &Store{Paths: paths, ProjectID: projectID, SolSession: solSession, Now: time.Now}
}

func (s *Store) Init() error {
	return s.Update(func(st *State) error { return nil })
}

func (s *Store) Read() (State, error) {
	l, err := lock.Acquire(s.Paths.Lock)
	if err != nil {
		return State{}, err
	}
	defer l.Close()
	return s.loadUnlocked()
}

func (s *Store) Update(fn func(*State) error) error {
	if err := config.EnsureDirectories(s.Paths); err != nil {
		return err
	}
	l, err := lock.Acquire(s.Paths.Lock)
	if err != nil {
		return err
	}
	defer l.Close()
	st, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	s.recoverStaleSending(&st)
	if err := fn(&st); err != nil {
		return err
	}
	return s.saveUnlocked(st)
}

func (s *Store) loadUnlocked() (State, error) {
	data, err := os.ReadFile(s.Paths.State)
	if errors.Is(err, os.ErrNotExist) {
		return NewForProject(s.ProjectID, s.SolSession), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	st := NewForProject(s.ProjectID, s.SolSession)
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	if st.Version != CurrentVersion {
		return State{}, fmt.Errorf("unsupported state version %d", st.Version)
	}
	if st.Workers == nil {
		st.Workers = map[string]*Worker{}
	}
	if st.Tasks == nil {
		st.Tasks = map[string]*Task{}
	}
	if st.Deliveries == nil {
		st.Deliveries = map[string]*Delivery{}
	}
	if st.Sol.Session == "" {
		st.Sol.Session = s.SolSession
	}
	if st.ProjectID == "" {
		// v0.1 states predate project namespaces and intentionally have no
		// project_id. Treat them as belonging to the store that loaded them.
		st.ProjectID = s.ProjectID
	} else if st.ProjectID != s.ProjectID {
		return State{}, fmt.Errorf("state belongs to project %q, not %q", st.ProjectID, s.ProjectID)
	}
	return st, nil
}

func (s *Store) saveUnlocked(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.Paths.State), ".state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Paths.State)
}

func (s *Store) recoverStaleSending(st *State) {
	now := s.Now()
	for _, d := range st.Deliveries {
		if d.Status == DeliverySending && !d.ReservedAt.IsZero() && now.Sub(d.ReservedAt) > 2*time.Minute {
			d.Status = DeliveryPending
			d.LastError = "recovered stale delivery reservation"
		}
	}
	if st.Sol.ReservedDelivery != "" {
		d, ok := st.Deliveries[st.Sol.ReservedDelivery]
		if !ok || d.Status != DeliverySending {
			st.Sol.ReservedDelivery = ""
			st.Sol.Busy = st.Sol.TurnID != ""
		}
	}
}

func ActiveTaskForWorker(st *State, worker string) *Task {
	var selected *Task
	for _, task := range st.Tasks {
		if task.WorkerSession != worker || task.Status != TaskRunning {
			continue
		}
		if selected == nil || task.CreatedAt.After(selected.CreatedAt) {
			selected = task
		}
	}
	return selected
}

func RunningTasks(st *State) []*Task {
	out := make([]*Task, 0)
	for _, task := range st.Tasks {
		if task.Status == TaskRunning {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func PendingDeliveries(st *State) []*Delivery {
	out := make([]*Delivery, 0)
	for _, delivery := range st.Deliveries {
		if delivery.Status == DeliveryPending {
			out = append(out, delivery)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
