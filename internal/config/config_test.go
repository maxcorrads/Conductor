package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultGoalRecoverySettingsAreValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.GoalPrefixDelayMS <= 0 || cfg.GoalDispatchTimeoutMS <= 0 || cfg.GoalReconcileDelayMS <= 0 {
		t.Fatalf("goal recovery defaults must be enabled: %+v", cfg)
	}
}

func TestLoadRejectsVersionOneConfig(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		Home:        dir,
		Config:      filepath.Join(dir, "config.json"),
		State:       filepath.Join(dir, "state.json"),
		Lock:        filepath.Join(dir, "state.lock"),
		TasksDir:    filepath.Join(dir, "tasks"),
		HandoffsDir: filepath.Join(dir, "handoffs"),
		CacheDir:    filepath.Join(dir, "cache"),
		LogsDir:     filepath.Join(dir, "logs"),
		Log:         filepath.Join(dir, "logs", "conductor.log"),
	}
	legacy := `{
  "version": 1,
  "brain_session": "brain",
  "worker_session_pattern": "^worker-[1-9][0-9]*$",
  "tmux_command": "tmux",
  "delivery_delay_ms": 180,
  "inline_goal_max_chars": 3500,
  "terminal_goal_statuses": ["complete", "blocked"],
  "transcript_tail_bytes": 33554432
}`
	if err := os.WriteFile(paths.Config, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err == nil {
		t.Fatal("expected version 1 configuration to be rejected")
	}
}

func TestLoadUsesDispatchTimeoutForConfigWrittenBeforeZeroPointFourPointOne(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Home: dir, Config: filepath.Join(dir, "config.json")}
	legacy := `{
  "version": 2,
  "brain_session": "brain",
  "worker_session_pattern": "^worker-[1-9][0-9]*$",
  "tmux_command": "tmux",
  "delivery_delay_ms": 180,
  "goal_prefix_delay_ms": 75,
  "goal_replace_probe_ms": 1200,
  "goal_reconcile_delay_ms": 1500,
  "inline_goal_max_chars": 3500,
  "terminal_goal_statuses": ["complete", "blocked"],
  "transcript_tail_bytes": 33554432
}`
	if err := os.WriteFile(paths.Config, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoalDispatchTimeoutMS != 10_000 {
		t.Fatalf("dispatch timeout = %d, want migrated default 10000", cfg.GoalDispatchTimeoutMS)
	}
}

func TestValidateRejectsDefaultSessionsThatOverlapNamedProjects(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(cfg *Config) { cfg.BrainSession = "demo--brain" },
		func(cfg *Config) { cfg.WorkerSessionPattern = `^.*--worker-[1-9][0-9]*$` },
	} {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("ambiguous config unexpectedly valid: %+v", cfg)
		}
	}
}
