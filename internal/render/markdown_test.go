package render

import (
	"strings"
	"testing"
	"time"

	"github.com/dejanr/video-transcribe/internal/source"
	"github.com/dejanr/video-transcribe/internal/whisper"
)

func TestMarkdownIncludesFrontmatterAndTranscript(t *testing.T) {
	metadata := source.Metadata{
		ID:           "abc123",
		Title:        "Example Video",
		Channel:      "Example Channel",
		Duration:     90,
		WebpageURL:   "https://example.com/watch?v=abc123",
		ExtractorKey: "Youtube",
	}
	output := whisper.Output{}
	output.Result.Language = "en"
	output.Transcription = []whisper.Segment{
		segment(0, 15000, "Hello there"),
		segment(16000, 30000, "General Kenobi"),
	}

	markdown := Markdown(metadata, output, Options{
		ModelName:         "base",
		RequestedLanguage: "auto",
		DetectedLanguage:  "en",
		GeneratedAt:       time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		ChunkSeconds:      45,
		SourceURL:         metadata.WebpageURL,
		RawTranscriptPath: "transcript.raw.json",
	})

	checks := []string{
		`title: "Example Video"`,
		`media_id: "abc123"`,
		`source_platform: "youtube"`,
		`model: "base"`,
		`raw_transcript: "transcript.raw.json"`,
		"Review the raw JSON transcript when you need segment-level timestamps.",
		"# Example Video",
		"## Transcript",
		"#### 00:00 - 00:30",
		"Hello there General Kenobi",
	}
	for _, check := range checks {
		if !strings.Contains(markdown, check) {
			t.Fatalf("markdown missing %q\n%s", check, markdown)
		}
	}
}

func TestMarkdownOmitsRawTranscriptWhenNotKept(t *testing.T) {
	metadata := source.Metadata{ID: "abc123", Title: "Example Video", Duration: 30}
	output := whisper.Output{}
	output.Result.Language = "en"
	output.Transcription = []whisper.Segment{segment(0, 5000, "Hello there")}

	markdown := Markdown(metadata, output, Options{
		ModelName:         "base",
		RequestedLanguage: "auto",
		DetectedLanguage:  "en",
		GeneratedAt:       time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		ChunkSeconds:      45,
	})

	if strings.Contains(markdown, "raw_transcript:") {
		t.Fatalf("markdown should omit raw_transcript when artifacts are not kept\n%s", markdown)
	}
	if !strings.Contains(markdown, "Re-run with --keep if you also want to keep raw audio and JSON artifacts.") {
		t.Fatalf("markdown missing keep hint\n%s", markdown)
	}
}

func segment(from int, to int, text string) whisper.Segment {
	var value whisper.Segment
	value.Offsets.From = from
	value.Offsets.To = to
	value.Text = text
	return value
}
