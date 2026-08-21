package project

import "testing"

func TestParseDefaultSessions(t *testing.T) {
	for _, tc := range []struct {
		session string
		role    Role
		alias   string
	}{
		{"brain", RoleBrain, "brain"},
		{"worker-1", RoleWorker, "worker-1"},
		{"worker-12", RoleWorker, "worker-12"},
	} {
		got, ok := ParseSession(tc.session, "brain", `^worker-[1-9][0-9]*$`)
		if !ok || got.ProjectID != DefaultID || got.Role != tc.role || got.Alias != tc.alias {
			t.Fatalf("ParseSession(%q) = %+v, %v", tc.session, got, ok)
		}
	}
}

func TestParseNamedSessions(t *testing.T) {
	for _, tc := range []struct {
		session string
		role    Role
		alias   string
	}{
		{"project1--brain", RoleBrain, "brain"},
		{"project1--worker-1", RoleWorker, "worker-1"},
		{"project2-books--worker-23", RoleWorker, "worker-23"},
	} {
		got, ok := ParseSession(tc.session, "brain", `^worker-[1-9][0-9]*$`)
		if !ok || got.ProjectID == DefaultID || got.Role != tc.role || got.Alias != tc.alias || got.Physical != tc.session {
			t.Fatalf("ParseSession(%q) = %+v, %v", tc.session, got, ok)
		}
	}
}

func TestWorkerSessionAcceptsAliasOrPhysicalName(t *testing.T) {
	for _, input := range []string{"worker-2", "project1--worker-2"} {
		physical, alias, err := WorkerSession("project1", input, `^worker-[1-9][0-9]*$`)
		if err != nil || physical != "project1--worker-2" || alias != "worker-2" {
			t.Fatalf("WorkerSession(%q) = %q, %q, %v", input, physical, alias, err)
		}
	}
}

func TestProjectValidationRejectsAmbiguousNames(t *testing.T) {
	for _, input := range []string{"", "Project1", "a--b", "-foo", "foo-", "foo_bar"} {
		_, err := NormalizeID(input)
		if input == "Project1" {
			// Names are normalized to lowercase for convenience.
			if err != nil {
				t.Fatalf("NormalizeID(%q): %v", input, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("NormalizeID(%q) unexpectedly succeeded", input)
		}
	}
}

func TestNamedSessionsCannotBeCapturedByDefaultConfiguration(t *testing.T) {
	for _, session := range []string{"demo--brain", "demo--worker-1"} {
		parsed, ok := ParseSession(session, "demo--brain", `^.*--worker-[1-9][0-9]*$`)
		if !ok || parsed.ProjectID != "demo" {
			t.Fatalf("named session routed incorrectly: %+v, %v", parsed, ok)
		}
	}
	if _, _, err := WorkerSession(DefaultID, "demo--worker-1", `^.*--worker-[1-9][0-9]*$`); err == nil {
		t.Fatal("default worker accepted a named-project session")
	}
}
