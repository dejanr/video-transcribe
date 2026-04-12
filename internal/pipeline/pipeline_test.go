package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"  Hello, World!  ":            "hello-world",
		"7 of 9 Reactivates Borg Cube": "7-of-9-reactivates-borg-cube",
		"***":                          "",
		"This is a very long title that should be trimmed down to a safe folder slug for artifacts": "this-is-a-very-long-title-that-should-be-trimmed-down-to-a-safe",
	}

	for input, want := range tests {
		if got := slugify(input); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRunValidatesGPUDevice(t *testing.T) {
	_, err := Run(Config{URL: "https://example.com/watch?v=test", GPUDevice: -1})
	if err == nil {
		t.Fatalf("expected error for negative GPU device")
	}
	if err.Error() != "gpu-device must be >= 0" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneWorkspaceKeepsOnlyMarkdown(t *testing.T) {
	workspaceDir := t.TempDir()
	markdownPath := filepath.Join(workspaceDir, "transcript.md")
	paths := []string{
		markdownPath,
		filepath.Join(workspaceDir, "metadata.json"),
		filepath.Join(workspaceDir, "audio.wav"),
		filepath.Join(workspaceDir, "transcript.raw.json"),
		filepath.Join(workspaceDir, "tmp", "run-1", "transcript.md"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll returned error: %v", err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}

	if err := pruneWorkspace(workspaceDir, markdownPath); err != nil {
		t.Fatalf("pruneWorkspace returned error: %v", err)
	}

	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "transcript.md" {
		t.Fatalf("expected only transcript.md to remain, got %+v", entries)
	}
}
