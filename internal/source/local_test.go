package source

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio #1 %.wav")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	localhostURL := (&url.URL{Scheme: "file", Host: "localhost", Path: path}).String()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativePath, err := filepath.Rel(workingDir, path)
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{path, relativePath, fileURL, localhostURL} {
		got, err := localPath(input)
		if err != nil || got != path {
			t.Errorf("localPath(%q) = %q, %v; want %q", input, got, err, path)
		}
	}
	for _, input := range []string{"https://example.com/watch?v=123", "https://youtu.be/123"} {
		got, err := localPath(input)
		if err != nil || got != "" {
			t.Errorf("localPath(%q) = %q, %v; want remote source", input, got, err)
		}
	}
	for _, input := range []string{
		filepath.Dir(path),
		filepath.Join(filepath.Dir(path), "missing.wav"),
		"file://remote" + path,
		"file:relative.wav",
		fileURL + "?query=1",
		fileURL + "#fragment",
		"file:///bad%zz",
	} {
		if _, err := localPath(input); err == nil {
			t.Errorf("localPath(%q) should fail", input)
		}
	}
}

func TestFetchLocalMetadata(t *testing.T) {
	stubCommand(t, "ffprobe", `{"format":{"duration":"12.25"},"streams":[{"codec_type":"audio"}]}`)
	path := filepath.Join(t.TempDir(), "recording.wav")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := Client{}
	ctx := context.Background()
	metadata, err := client.FetchMetadata(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	if metadata.Title != "recording" || metadata.Duration != 13 || metadata.Platform() != "local" || metadata.WebpageURL != fileURL || metadata.OriginalURL != fileURL {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if !strings.HasPrefix(metadata.ID, "local-") {
		t.Fatalf("unexpected ID: %q", metadata.ID)
	}
	fromURL, err := client.FetchMetadata(ctx, fileURL)
	if err != nil || fromURL.ID != metadata.ID {
		t.Fatalf("file URL should resolve to same identity: %+v, %v", fromURL, err)
	}
	otherPath := filepath.Join(t.TempDir(), "recording.wav")
	if err := os.WriteFile(otherPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	other, err := client.FetchMetadata(ctx, otherPath)
	if err != nil || other.ID == metadata.ID {
		t.Fatalf("different paths should have different identities: %+v, %v", other, err)
	}
	if err := os.WriteFile(path, []byte("modified recording"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified, err := client.FetchMetadata(ctx, path)
	if err != nil || modified.ID == metadata.ID {
		t.Fatalf("modified file should have a new identity: %+v, %v", modified, err)
	}
}

func TestFetchLocalMetadataRejectsInvalidMedia(t *testing.T) {
	for name, output := range map[string]string{
		"no audio":         `{"format":{"duration":"1"},"streams":[{"codec_type":"video"}]}`,
		"invalid duration": `{"format":{"duration":"N/A"},"streams":[{"codec_type":"audio"}]}`,
		"invalid JSON":     `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			stubCommand(t, "ffprobe", output)
			path := filepath.Join(t.TempDir(), "recording.wav")
			if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := (Client{}).FetchMetadata(context.Background(), path); err == nil {
				t.Fatal("expected metadata error")
			}
		})
	}
}

func TestDownloadLocalAudioPreservesOriginalWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.wav")
	original := "original audio"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	normalized := filepath.Join(workspace, "audio.wav")
	if err := os.WriteFile(normalized, []byte("normalized audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	got, err := (Client{}).DownloadAudio(context.Background(), fileURL, tempDir, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(tempDir, "source.wav") {
		t.Fatalf("unexpected source path: %q", got)
	}
	for file, want := range map[string]string{
		path: original, got: original, filepath.Join(workspace, "source.wav"): original, normalized: "normalized audio",
	} {
		body, err := os.ReadFile(file)
		if err != nil || string(body) != want {
			t.Errorf("ReadFile(%q) = %q, %v; want %q", file, body, err, want)
		}
	}
}

func TestRemoteMetadataStillUsesYTDLP(t *testing.T) {
	stubCommand(t, "yt-dlp", `{"id":"abc","title":"Remote video"}`)
	input := "https://example.com/watch?v=abc"
	metadata, err := (Client{}).FetchMetadata(context.Background(), input)
	if err != nil || metadata.ID != "abc" || metadata.WebpageURL != input || metadata.OriginalURL != input {
		t.Fatalf("unexpected remote metadata: %+v, %v", metadata, err)
	}
}

func stubCommand(t *testing.T, name string, output string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!" + sh + "\nprintf '%s' '" + output + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
