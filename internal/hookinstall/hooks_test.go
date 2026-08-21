package hookinstall

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPreservesExistingHooksAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	existing := `{
  "description": "existing",
  "hooks": {
    "Stop": [{"hooks": [{"type":"command","command":"echo existing"}]}]
  }
}`
	path := filepath.Join(home, "hooks.json")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Install("/tmp/conductor")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Backup == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("echo existing")) || !bytes.Contains(data, []byte("hook stop")) ||
		!bytes.Contains(data, []byte("hook post-tool-use")) || !bytes.Contains(data, []byte("^update_goal$")) {
		t.Fatalf("installed hooks did not preserve content: %s", data)
	}
	second, err := Install("/tmp/conductor")
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("second install should be idempotent")
	}
}

func TestUninstallRemovesOnlyConductorHandlers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if _, err := Install("/tmp/conductor"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	hooks := root["hooks"].(map[string]any)
	hooks["Stop"] = append(hooks["Stop"].([]any), map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "echo keep"},
		map[string]any{"type": "command", "command": "echo hook stop"},
		map[string]any{"type": "command", "command": "my-hook stop"},
	}})
	data, _ = json.MarshalIndent(root, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(final, []byte("'/tmp/conductor' hook stop")) || bytes.Contains(final, []byte("'/tmp/conductor' hook post-tool-use")) ||
		!bytes.Contains(final, []byte("echo keep")) || !bytes.Contains(final, []byte("echo hook stop")) || !bytes.Contains(final, []byte("my-hook stop")) {
		t.Fatalf("unexpected final hooks: %s", final)
	}
}

func TestConductorCommandRecognitionRequiresExactExecutableAndArguments(t *testing.T) {
	for _, command := range []string{
		"echo hook stop",
		"my-hook stop",
		"echo conductor hook stop",
		"'/tmp/not-conductor' hook stop",
		"'/tmp/conductor' hook stop --extra",
		"'/tmp/conductor'; echo hook stop",
	} {
		if isConductorCommand(command, "Stop") {
			t.Fatalf("non-Conductor command was accepted: %q", command)
		}
	}
	if !isConductorCommand("'/tmp/conductor' hook stop", "Stop") {
		t.Fatal("exact Conductor command was rejected")
	}
	if !isConductorCommand("/tmp/conductor hook stop", "Stop") {
		t.Fatal("legacy unquoted Conductor command was rejected")
	}
}

func TestInstallRejectsMalformedTopLevelHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "hooks.json")
	original := []byte(`{"hooks":[]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("/tmp/conductor"); err == nil {
		t.Fatal("expected malformed hooks to be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("malformed file was modified: %s", after)
	}
}

func TestInstallRejectsMalformedEventGroups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "hooks.json")
	original := []byte(`{"hooks":{"Stop":{}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("/tmp/conductor"); err == nil {
		t.Fatal("expected malformed event groups to be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("malformed file was modified: %s", after)
	}
}
