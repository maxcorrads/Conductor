package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GoalEvent struct {
	Status    string
	Objective string
	ThreadID  string
	Timestamp time.Time
	Source    string
}

type SessionProfile struct {
	Model  string
	Effort string
}

// LatestGoalEvent reads only the tail of a Codex JSONL transcript. The transcript
// is explicitly not a stable Codex API, so callers should keep a manual fallback.
func LatestGoalEvent(path, sessionID, expectedObjective string, since time.Time, maxBytes int64) (GoalEvent, bool, error) {
	if strings.TrimSpace(path) == "" {
		return GoalEvent{}, false, nil
	}
	if maxBytes <= 0 {
		maxBytes = 32 * 1024 * 1024
	}
	lines, err := readTailLines(path, maxBytes)
	if err != nil {
		return GoalEvent{}, false, err
	}

	var exact, threadWithoutObjective, objectiveMatch, fallback *GoalEvent
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var root any
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			continue
		}
		timestamp := findTimestamp(root)
		if !since.IsZero() && !timestamp.IsZero() && timestamp.Before(since.Add(-2*time.Second)) {
			continue
		}
		for _, candidate := range findGoalEvents(root, timestamp) {
			candidate.Source = "transcript"
			threadOK := sessionID == "" || candidate.ThreadID == "" || candidate.ThreadID == sessionID
			objectiveOK := expectedObjective == "" || candidate.Objective == "" || sameObjective(candidate.Objective, expectedObjective)

			copyCandidate := candidate
			if exact == nil && threadOK && objectiveOK {
				exact = &copyCandidate
			}
			if threadWithoutObjective == nil && threadOK && candidate.Objective == "" {
				threadWithoutObjective = &copyCandidate
			}
			if objectiveMatch == nil && objectiveOK {
				objectiveMatch = &copyCandidate
			}
			if fallback == nil {
				fallback = &copyCandidate
			}
		}
		if exact != nil {
			return *exact, true, nil
		}
	}
	if threadWithoutObjective != nil {
		return *threadWithoutObjective, true, nil
	}
	if objectiveMatch != nil {
		return *objectiveMatch, true, nil
	}
	if sessionID == "" && expectedObjective == "" && fallback != nil {
		return *fallback, true, nil
	}
	return GoalEvent{}, false, nil
}

// LatestGoalEventForThread returns the newest persisted goal associated with a
// thread without filtering by objective. It lets callers distinguish a truly
// absent goal from a different active goal that must not be treated as an
// implicit completion of the current task.
func LatestGoalEventForThread(path, sessionID string, since time.Time, maxBytes int64) (GoalEvent, bool, error) {
	if strings.TrimSpace(path) == "" {
		return GoalEvent{}, false, nil
	}
	if maxBytes <= 0 {
		maxBytes = 32 * 1024 * 1024
	}
	lines, err := readTailLines(path, maxBytes)
	if err != nil {
		return GoalEvent{}, false, err
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var root any
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			continue
		}
		timestamp := findTimestamp(root)
		if !since.IsZero() && !timestamp.IsZero() && timestamp.Before(since.Add(-2*time.Second)) {
			continue
		}
		for _, candidate := range findGoalEvents(root, timestamp) {
			if sessionID != "" && candidate.ThreadID != "" && candidate.ThreadID != sessionID {
				continue
			}
			candidate.Source = "transcript"
			return candidate, true, nil
		}
	}
	return GoalEvent{}, false, nil
}

func GoalStatusFromSQLite(sessionID string) (GoalEvent, bool, error) {
	event, found, _, err := GoalStatusFromSQLiteChecked(sessionID)
	return event, found, err
}

// GoalStatusFromSQLiteChecked reports whether a compatible local database was
// actually queried. Callers that infer absence must distinguish "no matching
// row" from "no readable goal source was available".
func GoalStatusFromSQLiteChecked(sessionID string) (GoalEvent, bool, bool, error) {
	if sessionID == "" {
		return GoalEvent{}, false, false, nil
	}
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return GoalEvent{}, false, false, nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return GoalEvent{}, false, false, err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	candidates := []string{
		filepath.Join(codexHome, "sqlite", "goals_1.sqlite"),
		filepath.Join(codexHome, "goals_1.sqlite"),
		filepath.Join(codexHome, "state_5.sqlite"),
		filepath.Join(codexHome, "state_4.sqlite"),
		filepath.Join(codexHome, "state_3.sqlite"),
	}
	query := "PRAGMA query_only=ON; PRAGMA busy_timeout=100; SELECT status || char(9) || objective FROM thread_goals WHERE thread_id='" + sqlQuote(sessionID) + "' LIMIT 1;"
	var errs []string
	checked := false
	for _, db := range candidates {
		if _, err := os.Stat(db); err != nil {
			continue
		}
		checked = true
		out, err := exec.Command(sqlite, "-readonly", "-noheader", db, query).CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", filepath.Base(db), strings.TrimSpace(string(out))))
			continue
		}
		line := strings.TrimSpace(string(out))
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		event := GoalEvent{Status: normalizeStatus(parts[0]), ThreadID: sessionID, Source: "sqlite"}
		if len(parts) == 2 {
			event.Objective = parts[1]
		}
		return event, event.Status != "", true, nil
	}
	if len(errs) > 0 {
		return GoalEvent{}, false, checked, errors.New(strings.Join(errs, "; "))
	}
	return GoalEvent{}, false, checked, nil
}

// SessionProfilesFromSQLiteChecked reads Codex's current model selection for
// known thread ids. The local database is not a stable Codex API, so callers
// must treat unavailable columns, databases, or sqlite as missing metadata.
func SessionProfilesFromSQLiteChecked(sessionIDs []string) (map[string]SessionProfile, bool, error) {
	profiles := map[string]SessionProfile{}
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		unique = append(unique, sessionID)
	}
	if len(unique) == 0 {
		return profiles, false, nil
	}

	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return profiles, false, nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return profiles, false, homeErr
		}
		codexHome = filepath.Join(home, ".codex")
	}

	quoted := make([]string, 0, len(unique))
	for _, sessionID := range unique {
		quoted = append(quoted, "'"+sqlQuote(sessionID)+"'")
	}
	query := "PRAGMA query_only=ON; PRAGMA busy_timeout=100; " +
		"SELECT id || char(9) || COALESCE(model,'') || char(9) || COALESCE(reasoning_effort,'') " +
		"FROM threads WHERE id IN (" + strings.Join(quoted, ",") + ");"
	type stateDatabase struct {
		path    string
		version int
	}
	matches, globErr := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if globErr != nil {
		return profiles, false, globErr
	}
	candidates := make([]stateDatabase, 0, len(matches))
	for _, match := range matches {
		base := filepath.Base(match)
		rawVersion := strings.TrimSuffix(strings.TrimPrefix(base, "state_"), ".sqlite")
		version, parseErr := strconv.Atoi(rawVersion)
		if parseErr == nil && version > 0 {
			candidates = append(candidates, stateDatabase{path: match, version: version})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].version > candidates[j].version })
	checked := false
	for _, candidate := range candidates {
		if _, statErr := os.Stat(candidate.path); statErr != nil {
			continue
		}
		checked = true
		out, commandErr := exec.Command(sqlite, "-readonly", "-noheader", candidate.path, query).CombinedOutput()
		if commandErr != nil {
			return profiles, true, fmt.Errorf("%s: %s", filepath.Base(candidate.path), strings.TrimSpace(string(out)))
		}
		for _, line := range strings.Split(string(out), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			threadID := strings.TrimSpace(parts[0])
			if _, requested := seen[threadID]; !requested {
				continue
			}
			profile := SessionProfile{
				Model:  strings.TrimSpace(parts[1]),
				Effort: strings.TrimSpace(parts[2]),
			}
			if profile.Model != "" || profile.Effort != "" {
				profiles[threadID] = profile
			}
		}
		return profiles, true, nil
	}
	return profiles, checked, nil
}

func readTailLines(path string, maxBytes int64) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat transcript: %w", err)
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek transcript: %w", err)
	}
	reader := bufio.NewReader(file)
	if start > 0 {
		_, _ = reader.ReadBytes('\n') // discard partial first line
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read transcript tail: %w", err)
	}
	return bytes.Split(data, []byte{'\n'}), nil
}

func findGoalEvents(root any, timestamp time.Time) []GoalEvent {
	var out []GoalEvent
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if typeName, _ := stringValue(typed, "type", "event_type", "eventType"); normalizeType(typeName) == "thread_goal_updated" {
				goalMap, _ := typed["goal"].(map[string]any)
				status, _ := stringValue(goalMap, "status")
				if status == "" {
					status, _ = stringValue(typed, "status")
				}
				objective, _ := stringValue(goalMap, "objective")
				if objective == "" {
					objective, _ = stringValue(typed, "objective")
				}
				threadID, _ := stringValue(goalMap, "threadId", "thread_id", "threadID")
				if threadID == "" {
					threadID, _ = stringValue(typed, "threadId", "thread_id", "threadID")
				}
				status = normalizeStatus(status)
				if status != "" {
					out = append(out, GoalEvent{Status: status, Objective: objective, ThreadID: threadID, Timestamp: timestamp})
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}

func findTimestamp(root any) time.Time {
	m, ok := root.(map[string]any)
	if !ok {
		return time.Time{}
	}
	value, _ := stringValue(m, "timestamp", "time", "created_at", "createdAt")
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func stringValue(m map[string]any, keys ...string) (string, bool) {
	if m == nil {
		return "", false
	}
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed, true
			case json.Number:
				return typed.String(), true
			}
		}
	}
	return "", false
}

func normalizeType(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "_")
	return strings.ToLower(value)
}

func normalizeStatus(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func sameObjective(a, b string) bool {
	normalize := func(value string) string {
		return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	}
	return normalize(a) == normalize(b)
}

// ObjectivesMatch compares persisted goal text while ignoring whitespace-only
// differences. It is exported so compatibility fallbacks can reject stale goal
// database rows from another task.
func ObjectivesMatch(a, b string) bool { return sameObjective(a, b) }

func sqlQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }
