package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const CurrentVersion = 1

type Config struct {
	Version              int      `json:"version"`
	SolSession           string   `json:"sol_session"`
	WorkerSessionPattern string   `json:"worker_session_pattern"`
	TmuxCommand          string   `json:"tmux_command"`
	DeliveryDelayMS      int      `json:"delivery_delay_ms"`
	InlineGoalMaxChars   int      `json:"inline_goal_max_chars"`
	TerminalGoalStatuses []string `json:"terminal_goal_statuses"`
	TranscriptTailBytes  int64    `json:"transcript_tail_bytes"`
}

type Paths struct {
	Root        string
	Home        string
	ProjectsDir string
	Config      string
	State       string
	Lock        string
	TasksDir    string
	HandoffsDir string
	CacheDir    string
	LogsDir     string
	Log         string
}

func Default() Config {
	return Config{
		Version:              CurrentVersion,
		SolSession:           "sol",
		WorkerSessionPattern: `^luna-[1-9][0-9]*$`,
		TmuxCommand:          "tmux",
		DeliveryDelayMS:      180,
		// Codex currently caps persisted /goal objectives at 4,000 characters.
		// Leave room for the /goal prefix and future CLI-side bookkeeping; longer
		// objectives are stored in a private file and referenced by absolute path.
		InlineGoalMaxChars:   3_500,
		TerminalGoalStatuses: []string{"complete", "blocked"},
		TranscriptTailBytes:  32 * 1024 * 1024,
	}
}

func ResolvePaths() (Paths, error) {
	home := os.Getenv("CONDUCTOR_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home directory: %w", err)
		}
		home = filepath.Join(userHome, ".conductor")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve conductor home: %w", err)
	}
	return Paths{
		Root:        home,
		Home:        home,
		ProjectsDir: filepath.Join(home, "projects"),
		Config:      filepath.Join(home, "config.json"),
		State:       filepath.Join(home, "state.json"),
		Lock:        filepath.Join(home, "state.lock"),
		TasksDir:    filepath.Join(home, "tasks"),
		HandoffsDir: filepath.Join(home, "handoffs"),
		CacheDir:    filepath.Join(home, "cache"),
		LogsDir:     filepath.Join(home, "logs"),
		Log:         filepath.Join(home, "logs", "conductor.log"),
	}, nil
}

// ForProject returns isolated runtime paths for a project while keeping the
// global configuration file shared. The default project retains the v0.1 path
// layout for backwards compatibility.
func ForProject(base Paths, projectID string) Paths {
	if projectID == "" || projectID == "default" {
		return base
	}
	root := base.Root
	if root == "" {
		root = base.Home
	}
	projectsDir := base.ProjectsDir
	if projectsDir == "" {
		projectsDir = filepath.Join(root, "projects")
	}
	home := filepath.Join(projectsDir, projectID)
	return Paths{
		Root:        root,
		Home:        home,
		ProjectsDir: projectsDir,
		Config:      base.Config,
		State:       filepath.Join(home, "state.json"),
		Lock:        filepath.Join(home, "state.lock"),
		TasksDir:    filepath.Join(home, "tasks"),
		HandoffsDir: filepath.Join(home, "handoffs"),
		CacheDir:    filepath.Join(home, "cache"),
		LogsDir:     filepath.Join(home, "logs"),
		Log:         filepath.Join(home, "logs", "conductor.log"),
	}
}

func EnsureDirectories(paths Paths) error {
	for _, dir := range []string{paths.Home, paths.TasksDir, paths.HandoffsDir, paths.CacheDir, paths.LogsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func Load(paths Paths) (Config, error) {
	data, err := os.ReadFile(paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", paths.Config, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(paths Paths, cfg Config) error {
	if err := EnsureDirectories(paths); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(paths.Config, data, 0o600)
}

func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d (expected %d)", c.Version, CurrentVersion)
	}
	if c.SolSession == "" {
		return errors.New("sol_session cannot be empty")
	}
	if c.WorkerSessionPattern == "" {
		return errors.New("worker_session_pattern cannot be empty")
	}
	if c.TmuxCommand == "" {
		return errors.New("tmux_command cannot be empty")
	}
	if c.DeliveryDelayMS < 0 || c.DeliveryDelayMS > 10_000 {
		return errors.New("delivery_delay_ms must be between 0 and 10000")
	}
	if c.InlineGoalMaxChars < 256 || c.InlineGoalMaxChars > 3_900 {
		return errors.New("inline_goal_max_chars must be between 256 and 3900")
	}
	if c.TranscriptTailBytes < 1024*1024 {
		return errors.New("transcript_tail_bytes must be at least 1 MiB")
	}
	if len(c.TerminalGoalStatuses) == 0 {
		return errors.New("terminal_goal_statuses cannot be empty")
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
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
	return os.Rename(tmpName, path)
}
