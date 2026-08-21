package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedPromptsMatchEmbeddedPrompts(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"BRAIN.md", BrainPrompt},
		{"WORKER.md", WorkerPrompt},
	}
	for _, test := range tests {
		data, err := os.ReadFile(filepath.Join("..", "..", "prompts", test.path))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != test.want {
			t.Fatalf("%s differs from the embedded prompt", test.path)
		}
	}
}
