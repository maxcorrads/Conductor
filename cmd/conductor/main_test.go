package main

import "testing"

func TestParseGlobalProjectOption(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		project string
		command string
	}{
		{[]string{"--project", "Project1", "status"}, "project1", "status"},
		{[]string{"-p", "project2", "inbox"}, "project2", "inbox"},
		{[]string{"--project=project1-ios", "doctor"}, "project1-ios", "doctor"},
		{[]string{"status"}, "", "status"},
	} {
		project, rest, err := parseGlobalOptions(tc.args)
		if err != nil {
			t.Fatalf("parseGlobalOptions(%v): %v", tc.args, err)
		}
		if project != tc.project || len(rest) == 0 || rest[0] != tc.command {
			t.Fatalf("parseGlobalOptions(%v) = project=%q rest=%v", tc.args, project, rest)
		}
	}
}

func TestParseGlobalProjectOptionRejectsAmbiguousName(t *testing.T) {
	if _, _, err := parseGlobalOptions([]string{"--project", "a--b", "status"}); err == nil {
		t.Fatal("expected invalid project to fail")
	}
}
