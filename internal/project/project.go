package project

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	DefaultID        = "default"
	SessionSeparator = "--"
)

var (
	projectIDRE   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	workerAliasRE = regexp.MustCompile(`^luna-[1-9][0-9]*$`)
)

type Role int

const (
	RoleUnknown Role = iota
	RoleSol
	RoleWorker
)

type Session struct {
	ProjectID string
	Role      Role
	Alias     string
	Physical  string
}

func NormalizeID(raw string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" {
		return "", errors.New("project name cannot be empty")
	}
	if id == DefaultID {
		return id, nil
	}
	if strings.Contains(id, SessionSeparator) || !projectIDRE.MatchString(id) {
		return "", fmt.Errorf("invalid project %q: use 1-64 lowercase letters, numbers, and single hyphens", raw)
	}
	return id, nil
}

func IsWorkerAlias(value string) bool {
	return workerAliasRE.MatchString(strings.TrimSpace(value))
}

func SolSession(projectID, defaultSol string) string {
	if projectID == DefaultID {
		return defaultSol
	}
	return projectID + SessionSeparator + "sol"
}

func WorkerSession(projectID, worker, defaultPattern string) (physical string, alias string, err error) {
	worker = strings.TrimSpace(worker)
	if projectID == DefaultID {
		re, compileErr := regexp.Compile(defaultPattern)
		if compileErr != nil {
			return "", "", fmt.Errorf("invalid default worker pattern: %w", compileErr)
		}
		if !re.MatchString(worker) {
			return "", "", fmt.Errorf("worker %q does not match %s", worker, defaultPattern)
		}
		return worker, worker, nil
	}

	prefix := projectID + SessionSeparator
	if strings.HasPrefix(worker, prefix) {
		worker = strings.TrimPrefix(worker, prefix)
	}
	if !IsWorkerAlias(worker) {
		return "", "", fmt.Errorf("worker %q must be luna-N for project %q", worker, projectID)
	}
	return prefix + worker, worker, nil
}

func WorkerPattern(projectID, defaultPattern string) string {
	if projectID == DefaultID {
		return defaultPattern
	}
	return `^` + regexp.QuoteMeta(projectID+SessionSeparator) + `luna-[1-9][0-9]*$`
}

func ParseSession(session, defaultSol, defaultWorkerPattern string) (Session, bool) {
	session = strings.TrimSpace(session)
	if session == "" {
		return Session{}, false
	}
	if session == defaultSol {
		return Session{ProjectID: DefaultID, Role: RoleSol, Alias: "sol", Physical: session}, true
	}
	if re, err := regexp.Compile(defaultWorkerPattern); err == nil && re.MatchString(session) {
		return Session{ProjectID: DefaultID, Role: RoleWorker, Alias: session, Physical: session}, true
	}

	parts := strings.Split(session, SessionSeparator)
	if len(parts) != 2 {
		return Session{}, false
	}
	projectID, err := NormalizeID(parts[0])
	if err != nil || projectID == DefaultID {
		return Session{}, false
	}
	switch {
	case parts[1] == "sol":
		return Session{ProjectID: projectID, Role: RoleSol, Alias: "sol", Physical: session}, true
	case IsWorkerAlias(parts[1]):
		return Session{ProjectID: projectID, Role: RoleWorker, Alias: parts[1], Physical: session}, true
	default:
		return Session{}, false
	}
}

func DisplayID(projectID string) string {
	if projectID == "" {
		return DefaultID
	}
	return projectID
}
