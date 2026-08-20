package project

import "testing"

func TestParseDefaultSessions(t *testing.T) {
	for _, tc := range []struct {
		session string
		role    Role
		alias   string
	}{
		{"sol", RoleSol, "sol"},
		{"luna-1", RoleWorker, "luna-1"},
		{"luna-12", RoleWorker, "luna-12"},
	} {
		got, ok := ParseSession(tc.session, "sol", `^luna-[1-9][0-9]*$`)
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
		{"project1--sol", RoleSol, "sol"},
		{"project1--luna-1", RoleWorker, "luna-1"},
		{"project2-books--luna-23", RoleWorker, "luna-23"},
	} {
		got, ok := ParseSession(tc.session, "sol", `^luna-[1-9][0-9]*$`)
		if !ok || got.ProjectID == DefaultID || got.Role != tc.role || got.Alias != tc.alias || got.Physical != tc.session {
			t.Fatalf("ParseSession(%q) = %+v, %v", tc.session, got, ok)
		}
	}
}

func TestWorkerSessionAcceptsAliasOrPhysicalName(t *testing.T) {
	for _, input := range []string{"luna-2", "project1--luna-2"} {
		physical, alias, err := WorkerSession("project1", input, `^luna-[1-9][0-9]*$`)
		if err != nil || physical != "project1--luna-2" || alias != "luna-2" {
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
