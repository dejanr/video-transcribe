package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dejanr/video-transcribe/internal/whisper"
	"github.com/dejanr/video-transcribe/internal/youtube"
)

type Options struct {
	ModelName         string
	RequestedLanguage string
	DetectedLanguage  string
	GeneratedAt       time.Time
	ChunkSeconds      int
	SourceURL         string
	RawTranscriptPath string
}

type chunk struct {
	StartMS int
	EndMS   int
	Text    string
}

func Markdown(metadata youtube.Metadata, output whisper.Output, options Options) string {
	if options.ChunkSeconds <= 0 {
		options.ChunkSeconds = 45
	}
	chunks := buildChunks(output.Transcription, options.ChunkSeconds)

	var b strings.Builder
	writeFrontmatter(&b, metadata, options)
	b.WriteString("\n")
	b.WriteString("# ")
	b.WriteString(metadata.Title)
	b.WriteString("\n\n")
	b.WriteString("Generated from the full YouTube audio track using yt-dlp, ffmpeg, and whisper.cpp.")
	if options.RawTranscriptPath != "" {
		b.WriteString(" Review the raw JSON transcript when you need segment-level timestamps.")
	} else {
		b.WriteString(" Re-run with --keep if you also want to keep raw audio and JSON artifacts.")
	}
	b.WriteString("\n\n")
	b.WriteString("## Transcript\n\n")

	if len(metadata.Chapters) > 0 {
		writeChapters(&b, metadata.Chapters, chunks)
	} else {
		writeChunkList(&b, chunks)
	}

	return b.String()
}

func writeFrontmatter(b *strings.Builder, metadata youtube.Metadata, options Options) {
	fmt.Fprintf(b, "---\n")
	fmt.Fprintf(b, "title: %q\n", metadata.Title)
	fmt.Fprintf(b, "video_id: %q\n", metadata.ID)
	fmt.Fprintf(b, "channel: %q\n", choose(metadata.Channel, metadata.Uploader))
	fmt.Fprintf(b, "source_url: %q\n", choose(metadata.WebpageURL, options.SourceURL))
	fmt.Fprintf(b, "duration_seconds: %d\n", metadata.Duration)
	fmt.Fprintf(b, "duration_human: %q\n", formatClock(metadata.Duration*1000))
	if metadata.UploadDate != "" {
		fmt.Fprintf(b, "upload_date: %q\n", metadata.UploadDate)
	}
	fmt.Fprintf(b, "transcribed_at: %q\n", options.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(b, "engine: %q\n", "whisper.cpp")
	fmt.Fprintf(b, "model: %q\n", options.ModelName)
	fmt.Fprintf(b, "language_requested: %q\n", options.RequestedLanguage)
	fmt.Fprintf(b, "language_detected: %q\n", options.DetectedLanguage)
	if options.RawTranscriptPath != "" {
		fmt.Fprintf(b, "raw_transcript: %q\n", options.RawTranscriptPath)
	}
	fmt.Fprintf(b, "---\n")
}

func writeChapters(b *strings.Builder, chapters []youtube.Chapter, chunks []chunk) {
	sorted := append([]youtube.Chapter(nil), chapters...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })

	for _, chapter := range sorted {
		chapterStart := int(chapter.StartTime * 1000)
		chapterEnd := int(chapter.EndTime * 1000)
		fmt.Fprintf(b, "### %s (%s)\n\n", strings.TrimSpace(chapter.Title), formatClock(chapterStart))
		var chapterChunks []chunk
		for _, c := range chunks {
			if c.EndMS < chapterStart || c.StartMS > chapterEnd {
				continue
			}
			chapterChunks = append(chapterChunks, c)
		}
		if len(chapterChunks) == 0 {
			continue
		}
		writeChunkList(b, chapterChunks)
	}
}

func writeChunkList(b *strings.Builder, chunks []chunk) {
	for _, c := range chunks {
		fmt.Fprintf(b, "#### %s - %s\n\n", formatClock(c.StartMS), formatClock(c.EndMS))
		fmt.Fprintf(b, "%s\n\n", c.Text)
	}
}

func buildChunks(segments []whisper.Segment, chunkSeconds int) []chunk {
	var chunks []chunk
	var current *chunk
	maxDurationMS := chunkSeconds * 1000
	maxChars := 900

	flush := func() {
		if current == nil {
			return
		}
		current.Text = strings.TrimSpace(current.Text)
		if current.Text != "" {
			chunks = append(chunks, *current)
		}
		current = nil
	}

	for _, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		if current == nil {
			current = &chunk{StartMS: segment.Offsets.From, EndMS: segment.Offsets.To, Text: text}
			continue
		}

		wouldExceedDuration := segment.Offsets.To-current.StartMS > maxDurationMS
		wouldExceedChars := len(current.Text)+1+len(text) > maxChars
		if wouldExceedDuration || wouldExceedChars {
			flush()
			current = &chunk{StartMS: segment.Offsets.From, EndMS: segment.Offsets.To, Text: text}
			continue
		}

		current.EndMS = segment.Offsets.To
		current.Text += " " + text
	}
	flush()
	return chunks
}

func formatClock(ms int) string {
	if ms < 0 {
		ms = 0
	}
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func choose(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
