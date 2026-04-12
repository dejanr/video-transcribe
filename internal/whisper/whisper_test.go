package whisper

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeFFmpegSegments(t *testing.T) {
	body := []byte("{\"start\":0,\"end\":3000,\"text\":\" hello there \",\"turn\":false}\n{\"start\":3000,\"end\":5500,\"text\":\" general kenobi \",\"turn\":false}\n")

	segments, err := decodeFFmpegSegments(body)
	if err != nil {
		t.Fatalf("decodeFFmpegSegments returned error: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0].Start != 0 || segments[0].End != 3000 {
		t.Fatalf("unexpected first segment timings: %+v", segments[0])
	}
}

func TestReadFFmpegOutput(t *testing.T) {
	path := t.TempDir() + "/transcript.json"
	body := "{\"start\":0,\"end\":3000,\"text\":\" hello there \",\"turn\":false}\n{\"start\":3000,\"end\":5500,\"text\":\" general kenobi \",\"turn\":false}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	output, err := readFFmpegOutput(path, "en")
	if err != nil {
		t.Fatalf("readFFmpegOutput returned error: %v", err)
	}
	if output.Result.Language != "en" {
		t.Fatalf("expected language en, got %q", output.Result.Language)
	}
	if len(output.Transcription) != 2 {
		t.Fatalf("expected 2 transcription segments, got %d", len(output.Transcription))
	}
	if output.Transcription[0].Text != "hello there" {
		t.Fatalf("unexpected normalized text: %q", output.Transcription[0].Text)
	}
	if output.Transcription[1].Offsets.From != 3000 || output.Transcription[1].Offsets.To != 5500 {
		t.Fatalf("unexpected offsets: %+v", output.Transcription[1].Offsets)
	}
}

func TestWhisperCLIArgs(t *testing.T) {
	args := whisperCLIArgs("/models/ggml.bin", "/tmp/audio.wav", "/tmp/transcript.raw", "", false)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-l auto") {
		t.Fatalf("expected auto language in args, got %q", joined)
	}
	if !strings.Contains(joined, "-ng") {
		t.Fatalf("expected -ng in args, got %q", joined)
	}
}

func TestFFmpegWhisperFilter(t *testing.T) {
	filter := ffmpegWhisperFilter("/models/ggml.bin", "en", "/tmp/transcript.raw.json", true, 2, 15)
	if !strings.Contains(filter, "use_gpu=true") {
		t.Fatalf("expected use_gpu=true in filter, got %q", filter)
	}
	if !strings.Contains(filter, "gpu_device=2") {
		t.Fatalf("expected gpu_device=2 in filter, got %q", filter)
	}
	if !strings.Contains(filter, "format=json") {
		t.Fatalf("expected format=json in filter, got %q", filter)
	}
}

func TestFormatMilliseconds(t *testing.T) {
	if got := formatMilliseconds(3723004); got != "01:02:03.004" {
		t.Fatalf("expected 01:02:03.004, got %q", got)
	}
}
