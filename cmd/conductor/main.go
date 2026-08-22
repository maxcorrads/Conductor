package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxcorrads/conductor/internal/app"
	"github.com/maxcorrads/conductor/internal/hookinstall"
	"github.com/maxcorrads/conductor/internal/project"
	"github.com/maxcorrads/conductor/internal/state"
	"github.com/maxcorrads/conductor/internal/tmux"
)

var version = "0.4.3"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "hook" {
		runHook(os.Args[2:])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "_deliver" {
		if err := runInternalDeliver(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "conductor:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "conductor:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	projectID, args, err := parseGlobalOptions(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	// Keep read-only, self-contained commands usable even when a local config is
	// temporarily invalid or CONDUCTOR_HOME is unavailable.
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("conductor", version)
		return nil
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	case "prompt":
		return runPrompt(args[1:])
	case "project", "projects":
		return runProject(args)
	case "gui":
		return runGUI(args[1:])
	}
	var a *app.App
	if projectID == "" {
		a, err = app.New()
	} else {
		a, err = app.NewForProject(projectID)
	}
	if err != nil {
		return err
	}
	switch args[0] {
	case "init":
		if err := a.Init(); err != nil {
			return err
		}
		fmt.Printf("Initialized project %s in %s\n", a.ProjectID, a.Paths.Home)
		return nil
	case "goal":
		return runGoal(a, args[1:])
	case "brain":
		return runBrain(a, args[1:])
	case "status":
		return runStatus(a, args[1:])
	case "inbox":
		return runInbox(a, args[1:])
	case "finish":
		return runFinish(a, args[1:])
	case "flush":
		return runFlush(a, args[1:])
	case "idle":
		if err := a.MarkBrainIdle(); err != nil {
			return err
		}
		fmt.Printf("Brain for %s marked idle; any stale delivery reservation was returned to the queue.\n", a.ProjectID)
		return nil
	case "hooks":
		return runHooks(a, args[1:])
	case "doctor":
		return runDoctor(a, args[1:])
	default:
		return fmt.Errorf("unknown command %q (run conductor help)", args[0])
	}
}

func runBrain(a *app.App, args []string) error {
	if len(args) == 0 || args[0] != "setup" {
		return errors.New("usage: conductor brain setup [--stdin | --file PATH | PROMPT]")
	}
	prompt, err := parsePayload(args[1:])
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("setup prompt cannot be empty")
	}
	if err := a.SendBrainSetup(prompt, func(pane tmux.Pane) error {
		content, captureErr := captureCodexPane(a.Config.TmuxCommand, pane.ID)
		if captureErr != nil {
			return captureErr
		}
		return validateBrainSetupPane(content)
	}); err != nil {
		return err
	}
	fmt.Printf("Setup prompt sent to Brain for %s.\n", a.ProjectID)
	return nil
}

func validateBrainSetupPane(content string) error {
	if paneShowsActiveTurn(content) {
		return errors.New("Brain is working; setup prompt was not sent")
	}
	if !paneShowsEmptyComposer(content) {
		return errors.New("Brain composer is not empty; setup prompt was not sent")
	}
	return nil
}

func captureCodexPane(tmuxCommand, pane string) (string, error) {
	output, err := exec.Command(tmuxCommand, "capture-pane", "-p", "-J", "-t", pane, "-S", "-30").CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	fallback, fallbackErr := exec.Command(tmuxCommand, "capture-pane", "-p", "-t", pane, "-S", "-30").CombinedOutput()
	if fallbackErr != nil {
		return "", fmt.Errorf("capture Brain pane: %w: %s", fallbackErr, strings.TrimSpace(string(fallback)))
	}
	return string(fallback), nil
}

func runGoal(a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: conductor goal <worker-N> [--stdin | --file PATH | OBJECTIVE]")
	}
	worker := args[0]
	payload, err := parsePayload(args[1:])
	if err != nil {
		return err
	}
	task, err := a.Delegate(worker, payload)
	if err != nil {
		return err
	}
	displayWorker := task.WorkerAlias
	if displayWorker == "" {
		displayWorker = task.WorkerSession
	}
	fmt.Printf("delegated %s\n", displayWorker)
	return nil
}

func runStatus(a *app.App, args []string) error {
	jsonOutput := false
	all := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--all":
			all = true
		default:
			return fmt.Errorf("unknown status option %q", arg)
		}
	}
	if all {
		return runAllStatus(jsonOutput)
	}
	if jsonOutput {
		data, err := a.StatusJSON()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	text, err := a.StatusText()
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func runInbox(a *app.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else {
			return fmt.Errorf("unknown inbox option %q", arg)
		}
	}
	st, err := a.Store.Read()
	if err != nil {
		return err
	}
	pending := state.PendingDeliveries(&st)
	if jsonOutput {
		data, marshalErr := json.MarshalIndent(map[string]any{"handoffs": pending}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(data))
		return nil
	}
	if len(pending) == 0 {
		fmt.Println("No pending handoffs.")
		return nil
	}
	for _, d := range pending {
		worker := d.WorkerAlias
		if worker == "" {
			worker = d.WorkerSession
		}
		fmt.Printf("%s  %-8s  task=%s  %s\n", d.ID, worker, d.TaskID, d.MessagePath)
	}
	return nil
}

func runFinish(a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: conductor finish <worker-N> [--task-id ID] [--stdin | --file PATH] [--status STATUS]")
	}
	worker := args[0]
	status := "manual"
	expectedTaskID := ""
	var payloadArgs []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--status" {
			if i+1 >= len(args) {
				return errors.New("--status requires a value")
			}
			status = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--status=") {
			status = strings.TrimPrefix(arg, "--status=")
			continue
		}
		if arg == "--task-id" {
			if i+1 >= len(args) {
				return errors.New("--task-id requires a value")
			}
			expectedTaskID = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--task-id=") {
			expectedTaskID = strings.TrimPrefix(arg, "--task-id=")
			continue
		}
		payloadArgs = append(payloadArgs, arg)
	}
	message := ""
	if len(payloadArgs) > 0 {
		var err error
		message, err = parsePayload(payloadArgs)
		if err != nil {
			return err
		}
	}
	id, err := a.FinishWorkerTask(worker, expectedTaskID, message, status)
	if err != nil {
		return err
	}
	fmt.Printf("finished %s; handoff %s queued or delivered\n", worker, id)
	return nil
}

func runFlush(a *app.App, args []string) error {
	force := false
	for _, arg := range args {
		if arg == "--force" {
			force = true
		} else {
			return fmt.Errorf("unknown flush option %q", arg)
		}
	}
	id, err := a.Flush(force)
	if err != nil {
		return err
	}
	if id == "" {
		fmt.Println("No pending handoffs.")
	} else {
		fmt.Println("delivered", id)
	}
	return nil
}

func runHooks(a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: conductor hooks install|uninstall|path")
	}
	switch args[0] {
	case "install":
		result, err := hookinstall.Install(a.Executable)
		if err != nil {
			return err
		}
		if result.Changed {
			fmt.Printf("Installed hooks in %s\n", result.Path)
			if result.Backup != "" {
				fmt.Printf("Backup: %s\n", result.Backup)
			}
		} else {
			fmt.Printf("Hooks already installed in %s\n", result.Path)
		}
		fmt.Println("Open /hooks in each Codex CLI session and trust the Conductor hooks.")
		return nil
	case "uninstall":
		result, err := hookinstall.Uninstall()
		if err != nil {
			return err
		}
		if result.Changed {
			fmt.Printf("Removed Conductor hooks from %s\n", result.Path)
			if result.Backup != "" {
				fmt.Printf("Backup: %s\n", result.Backup)
			}
		} else {
			fmt.Println("No Conductor hooks found.")
		}
		return nil
	case "path":
		path, err := hookinstall.CodexHooksPath()
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	default:
		return fmt.Errorf("unknown hooks command %q", args[0])
	}
}

func runPrompt(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: conductor prompt brain|worker")
	}
	switch args[0] {
	case "brain":
		fmt.Print(app.BrainPrompt)
	case "worker":
		fmt.Print(app.WorkerPrompt)
	default:
		return errors.New("usage: conductor prompt brain|worker")
	}
	return nil
}

type doctorCheck struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	OK    bool   `json:"ok"`
}

type doctorReport struct {
	Checks          []doctorCheck `json:"checks"`
	CriticalFailure bool          `json:"critical_failure"`
	Note            string        `json:"note"`
}

func runDoctor(a *app.App, args []string) error {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else {
			return fmt.Errorf("unknown doctor option %q", arg)
		}
	}
	report := collectDoctorReport(a)
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		if report.CriticalFailure {
			return errors.New("doctor found missing required setup")
		}
		return nil
	}
	for _, c := range report.Checks {
		mark := "OK"
		if !c.OK {
			mark = "WARN"
		}
		fmt.Printf("%-5s %-15s %s\n", mark, c.Name, c.Value)
	}
	fmt.Println("NOTE ", report.Note)
	if report.CriticalFailure {
		return errors.New("doctor found missing required setup")
	}
	return nil
}

func collectDoctorReport(a *app.App) doctorReport {
	checks := []doctorCheck{{"project", a.ProjectID, true}, {"platform", runtime.GOOS + "/" + runtime.GOARCH, runtime.GOOS == "darwin"}}
	if path, err := exec.LookPath(a.Config.TmuxCommand); err == nil {
		version, versionErr := a.Tmux.Version()
		checks = append(checks, doctorCheck{"tmux", path + " (" + version + ")", versionErr == nil})
	} else {
		checks = append(checks, doctorCheck{"tmux", "not found", false})
	}
	if path, err := exec.LookPath("codex"); err == nil {
		out, versionErr := exec.Command(path, "--version").CombinedOutput()
		checks = append(checks, doctorCheck{"codex", path + " (" + strings.TrimSpace(string(out)) + ")", versionErr == nil})
	} else {
		checks = append(checks, doctorCheck{"codex", "not found", false})
	}
	installed, hooksPath, hooksErr := hookinstall.Installed(a.Executable)
	checks = append(checks, doctorCheck{"hooks", hooksPath, hooksErr == nil && installed})
	checks = append(checks, doctorCheck{"config", a.Paths.Config, fileExists(a.Paths.Config)})

	sessions, sessionErr := a.Tmux.ListSessions()
	if sessionErr != nil {
		checks = append(checks, doctorCheck{"tmux sessions", sessionErr.Error(), false})
	} else {
		sort.Strings(sessions)
		checks = append(checks, doctorCheck{"tmux sessions", strings.Join(sessions, ", "), contains(sessions, a.BrainSession)})
	}
	goalState := detectCodexFeature("goals")
	checks = append(checks, doctorCheck{"Codex goals", goalState, strings.HasPrefix(goalState, "enabled")})
	hooksState := detectCodexFeature("hooks")
	checks = append(checks, doctorCheck{"Codex hooks", hooksState, strings.HasPrefix(hooksState, "enabled")})

	criticalFailure := false
	for _, c := range checks {
		if (c.Name == "tmux" || c.Name == "codex" || c.Name == "hooks" || c.Name == "Codex goals" || c.Name == "Codex hooks") && !c.OK {
			criticalFailure = true
		}
	}
	return doctorReport{
		Checks:          checks,
		CriticalFailure: criticalFailure,
		Note:            "Hook trust cannot be verified automatically; inspect /hooks inside Codex.",
	}
}

func runHook(args []string) {
	// Hook commands must always emit valid JSON on stdout. Any integration error is
	// logged and treated as non-blocking so Conductor cannot trap Codex in a loop.
	fmt.Print("{}")
	if len(args) != 1 {
		return
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024*1024))
	if err != nil {
		return
	}
	input, err := app.DecodeHookInput(data)
	if err != nil {
		return
	}
	a, err := app.NewForHook(input)
	if err != nil {
		return
	}
	if err := a.HandleHook(args[0], input); err != nil {
		a.Logf("hook %s failed: %v", args[0], err)
	}
}

func runInternalDeliver(args []string) error {
	if len(args) == 0 {
		return errors.New("missing delivery id")
	}
	deliveryID := args[0]
	delayMS := int64(0)
	attempt := 0
	projectID := ""
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--attempt" {
			if i+1 >= len(args) {
				return errors.New("--attempt requires a value")
			}
			parsed, err := strconv.Atoi(args[i+1])
			if err != nil || parsed <= 0 {
				return errors.New("--attempt requires a positive integer")
			}
			attempt = parsed
			i++
		} else if strings.HasPrefix(arg, "--attempt=") {
			parsed, err := strconv.Atoi(strings.TrimPrefix(arg, "--attempt="))
			if err != nil || parsed <= 0 {
				return errors.New("--attempt requires a positive integer")
			}
			attempt = parsed
		} else if arg == "--project" {
			if i+1 >= len(args) {
				return errors.New("--project requires a value")
			}
			projectID = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--project=") {
			projectID = strings.TrimPrefix(arg, "--project=")
		} else if arg == "--delay-ms" {
			if i+1 >= len(args) {
				return errors.New("--delay-ms requires a value")
			}
			parsed, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil {
				return err
			}
			delayMS = parsed
			i++
		} else if strings.HasPrefix(arg, "--delay-ms=") {
			parsed, err := strconv.ParseInt(strings.TrimPrefix(arg, "--delay-ms="), 10, 64)
			if err != nil {
				return err
			}
			delayMS = parsed
		} else {
			return fmt.Errorf("unknown option %q", arg)
		}
	}
	if attempt <= 0 {
		return errors.New("--attempt is required")
	}
	var a *app.App
	var err error
	if projectID == "" {
		a, err = app.New()
	} else {
		a, err = app.NewForProject(projectID)
	}
	if err != nil {
		return err
	}
	return a.DeliverLease(deliveryID, attempt, time.Duration(delayMS)*time.Millisecond)
}

func parsePayload(args []string) (payload string, err error) {
	stdinMode := false
	filePath := ""
	var text []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--stdin":
			stdinMode = true
		case arg == "--file":
			if i+1 >= len(args) {
				return "", errors.New("--file requires a path")
			}
			filePath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--file="):
			filePath = strings.TrimPrefix(arg, "--file=")
		case arg == "--":
			text = append(text, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			return "", fmt.Errorf("unknown option %q", arg)
		default:
			text = append(text, arg)
		}
	}
	sources := 0
	if stdinMode {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if len(text) > 0 {
		sources++
	}
	if sources > 1 {
		return "", errors.New("choose exactly one payload source: --stdin, --file, or positional text")
	}
	switch {
	case stdinMode:
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 16*1024*1024))
		if readErr != nil {
			return "", readErr
		}
		payload = string(data)
	case filePath != "":
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return "", readErr
		}
		payload = string(data)
	default:
		payload = strings.Join(text, " ")
	}
	if strings.TrimSpace(payload) == "" && len(args) > 0 {
		return "", errors.New("payload is empty")
	}
	return payload, nil
}

func detectCodexFeature(name string) string {
	if path, err := exec.LookPath("codex"); err == nil {
		if out, commandErr := exec.Command(path, "features", "list").CombinedOutput(); commandErr == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == name {
					switch strings.ToLower(fields[len(fields)-1]) {
					case "true", "enabled", "on":
						return "enabled"
					case "false", "disabled", "off":
						return "disabled"
					}
				}
			}
		}
	}

	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "unknown"
		}
		codexHome = filepath.Join(home, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err == nil {
		trueRE := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*true\s*(?:#.*)?$`)
		if trueRE.Match(data) {
			return "enabled"
		}
		falseRE := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*false\s*(?:#.*)?$`)
		if falseRE.Match(data) {
			return "disabled"
		}
	}
	if name == "goals" || name == "hooks" {
		return "enabled (current default; not explicitly overridden)"
	}
	return "unknown"
}

func contains(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func parseGlobalOptions(args []string) (string, []string, error) {
	projectID := ""
	for len(args) > 0 {
		switch {
		case args[0] == "--project" || args[0] == "-p":
			if len(args) < 2 {
				return "", nil, errors.New("--project requires a value")
			}
			projectID = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--project="):
			projectID = strings.TrimPrefix(args[0], "--project=")
			args = args[1:]
		default:
			if projectID != "" {
				normalized, err := project.NormalizeID(projectID)
				return normalized, args, err
			}
			return "", args, nil
		}
	}
	if projectID != "" {
		normalized, err := project.NormalizeID(projectID)
		return normalized, args, err
	}
	return "", args, nil
}

func runProject(args []string) error {
	// Accept both `conductor projects` and `conductor project list`.
	if args[0] == "projects" {
		if len(args) != 1 {
			return errors.New("usage: conductor projects")
		}
		return listProjects()
	}
	if len(args) < 2 {
		return errors.New("usage: conductor project list|init|sessions|delete [NAME]")
	}
	switch args[1] {
	case "list":
		if len(args) != 2 {
			return errors.New("usage: conductor project list")
		}
		return listProjects()
	case "init":
		if len(args) != 3 {
			return errors.New("usage: conductor project init NAME")
		}
		a, err := app.NewForProject(args[2])
		if err != nil {
			return err
		}
		if err := a.Init(); err != nil {
			return err
		}
		printProjectSessions(a)
		return nil
	case "sessions":
		if len(args) != 3 {
			return errors.New("usage: conductor project sessions NAME")
		}
		a, err := app.NewForProject(args[2])
		if err != nil {
			return err
		}
		printProjectSessions(a)
		return nil
	case "delete":
		if len(args) != 4 || args[3] != "--yes" {
			return errors.New("usage: conductor project delete NAME --yes")
		}
		projectID, err := project.NormalizeID(args[2])
		if err != nil {
			return err
		}
		ids, err := app.DiscoverProjectIDs()
		if err != nil {
			return err
		}
		if !contains(ids, projectID) {
			return fmt.Errorf("project %q is not initialized", projectID)
		}
		a, err := app.NewForProject(projectID)
		if err != nil {
			return err
		}
		if err := a.DeleteProject(); err != nil {
			return err
		}
		fmt.Printf("Deleted Conductor project %s. Workspaces and tmux sessions were not changed.\n", a.ProjectID)
		return nil
	default:
		return fmt.Errorf("unknown project command %q", args[1])
	}
}

func printProjectSessions(a *app.App) {
	fmt.Printf("Project: %s\n", a.ProjectID)
	fmt.Printf("Brain session: %s\n", a.BrainSession)
	if a.ProjectID == project.DefaultID {
		fmt.Println("Worker sessions: worker-1, worker-2, ...")
	} else {
		fmt.Printf("Worker sessions: %s--worker-1, %s--worker-2, ...\n", a.ProjectID, a.ProjectID)
	}
	fmt.Printf("State: %s\n", a.Paths.State)
}

func listProjects() error {
	ids, err := app.DiscoverProjectIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		a, appErr := app.NewForProject(id)
		if appErr != nil {
			return appErr
		}
		fmt.Printf("%-20s brain=%-28s state=%s\n", id, a.BrainSession, a.Paths.State)
	}
	return nil
}

func runAllStatus(jsonOutput bool) error {
	ids, err := app.DiscoverProjectIDs()
	if err != nil {
		return err
	}
	if jsonOutput {
		projects := make(map[string]any, len(ids))
		for _, id := range ids {
			a, appErr := app.NewForProject(id)
			if appErr != nil {
				return appErr
			}
			st, readErr := a.Store.Read()
			if readErr != nil {
				return readErr
			}
			projects[id] = st
		}
		data, marshalErr := json.MarshalIndent(map[string]any{"projects": projects}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(data))
		return nil
	}
	for index, id := range ids {
		a, appErr := app.NewForProject(id)
		if appErr != nil {
			return appErr
		}
		text, statusErr := a.StatusText()
		if statusErr != nil {
			return statusErr
		}
		if index > 0 {
			fmt.Println()
		}
		fmt.Print(text)
	}
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Conductor — visible Codex-to-Codex relay over tmux

Usage:
  conductor [--project NAME] init
  conductor goal <worker-N> [--stdin | --file PATH | OBJECTIVE]
  conductor brain setup [--stdin | --file PATH | PROMPT]
  conductor status [--json] [--all]
  conductor inbox [--json]
  conductor finish <worker-N> [--task-id ID] [--stdin | --file PATH] [--status STATUS]
  conductor flush [--force]
  conductor idle
  conductor hooks install|uninstall|path
  conductor prompt brain|worker
  conductor doctor [--json]
  conductor gui snapshot
  conductor project init NAME
  conductor project list
  conductor project sessions NAME
  conductor project delete NAME --yes
  conductor version

Project session convention:
  default:  brain, worker-1, worker-2, ...
  named:    NAME--brain, NAME--worker-1, NAME--worker-2, ...

Inside a named Brain session, conductor goal worker-1 ... automatically targets
NAME--worker-1. Outside tmux, select a project with --project NAME or -p NAME.

Typical delegation from Brain:
  cat <<'GOAL' | conductor goal worker-1 --stdin
  Implement the requested change. In the final handoff include tests and risks.
  GOAL
`)
}
