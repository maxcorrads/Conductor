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
	if cfg.GoalClearDelayMS <= 0 || cfg.GoalPrefixDelayMS <= 0 || cfg.GoalReplaceProbeMS <= 0 || cfg.GoalReconcileDelayMS <= 0 {
		t.Fatalf("goal recovery defaults must be enabled: %+v", cfg)
	}
}

func TestLoadLegacyConfigKeepsNewGoalRecoveryDefaults(t *testing.T) {
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
  "sol_session": "sol",
  "worker_session_pattern": "^luna-[1-9][0-9]*$",
  "tmux_command": "tmux",
  "delivery_delay_ms": 180,
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
	defaults := Default()
	if cfg.GoalClearDelayMS != defaults.GoalClearDelayMS ||
		cfg.GoalPrefixDelayMS != defaults.GoalPrefixDelayMS ||
		cfg.GoalReplaceProbeMS != defaults.GoalReplaceProbeMS ||
		cfg.GoalReconcileDelayMS != defaults.GoalReconcileDelayMS {
		t.Fatalf("legacy config lost new defaults: %+v", cfg)
	}
}
