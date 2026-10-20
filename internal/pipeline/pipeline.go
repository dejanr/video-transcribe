package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dejanr/video-transcribe/internal/audio"
	"github.com/dejanr/video-transcribe/internal/render"
	"github.com/dejanr/video-transcribe/internal/shell"
	"github.com/dejanr/video-transcribe/internal/source"
	"github.com/dejanr/video-transcribe/internal/whisper"
)

type Config struct {
	URL                       string
	OutputDir                 string
	ModelName                 string
	Backend                   string
	FFmpegWhisperQueueSeconds int
	Language                  string
	UseGPU                    bool
	GPUDevice                 int
	KeepArtifacts             bool
	Force                     bool
	ChunkSeconds              int
	Trace                     bool
}

type Result struct {
	Metadata          source.Metadata
	WorkspaceDir      string
	TempDir           string
	MarkdownPath      string
	RawTranscriptPath string
}

func Run(config Config) (Result, error) {
	if strings.TrimSpace(config.URL) == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	if strings.TrimSpace(config.OutputDir) == "" {
		config.OutputDir = "./output"
	}
	if strings.TrimSpace(config.ModelName) == "" {
		config.ModelName = "large-v3-turbo"
	}
	if strings.TrimSpace(config.Backend) == "" {
		config.Backend = "whisper-cli"
	}
	if strings.TrimSpace(config.Language) == "" {
		config.Language = "en"
	}
	if config.GPUDevice < 0 {
		return Result{}, fmt.Errorf("gpu-device must be >= 0")
	}
	if config.FFmpegWhisperQueueSeconds <= 0 {
		config.FFmpegWhisperQueueSeconds = 15
	}
	if config.ChunkSeconds <= 0 {
		config.ChunkSeconds = 45
	}
	if strings.HasSuffix(config.ModelName, ".en") && config.Language != "auto" && config.Language != "en" {
		return Result{}, fmt.Errorf("model %q only supports English; use --language en/auto or pick a multilingual model", config.ModelName)
	}
	if config.Backend != "whisper-cli" && config.Backend != "ffmpeg-whisper" {
		return Result{}, fmt.Errorf("unsupported backend %q; use whisper-cli or ffmpeg-whisper", config.Backend)
	}

	ctx := context.Background()
	runner := shell.Runner{Trace: config.Trace}
	sourceClient := source.Client{Runner: runner}
	whisperClient := whisper.Client{Runner: runner}
	normalizer := audio.Normalizer{Runner: runner}
	totalStartedAt := time.Now()

	fmt.Println("==> fetching source metadata")
	metadataStartedAt := time.Now()
	metadata, err := sourceClient.FetchMetadata(ctx, config.URL)
	if err != nil {
		return Result{}, err
	}
	traceStage(config.Trace, "fetch metadata", metadataStartedAt)
	if config.Trace {
		fmt.Printf("[trace] metadata duration=%s chapters=%d channel=%q\n", formatSeconds(metadata.Duration), len(metadata.Chapters), metadata.Channel)
	}

	workspaceDir, err := workspaceDir(config.OutputDir, metadata)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create workspace dir: %w", err)
	}
	tempDir, err := shell.NewRunDir(workspaceDir)
	if err != nil {
		return Result{}, err
	}

	tempMetadataPath := filepath.Join(tempDir, "metadata.json")
	metadataPath := filepath.Join(workspaceDir, "metadata.json")
	if err := shell.WriteJSON(tempMetadataPath, metadata); err != nil {
		return Result{}, err
	}
	if err := shell.WriteJSON(metadataPath, metadata); err != nil {
		return Result{}, err
	}

	fmt.Println("==> downloading audio")
	audioStartedAt := time.Now()
	audioPath, err := sourceClient.DownloadAudio(ctx, config.URL, tempDir, workspaceDir, config.Force)
	if err != nil {
		return Result{}, err
	}
	traceStage(config.Trace, "download audio", audioStartedAt)
	traceFile(config.Trace, "downloaded-audio", audioPath)

	fmt.Println("==> normalizing audio for speech-to-text")
	normalizeStartedAt := time.Now()
	transcriptionInputPath, err := normalizer.Normalize(ctx, audioPath, tempDir, workspaceDir, config.Force)
	if err != nil {
		return Result{}, err
	}
	traceStage(config.Trace, "normalize audio", normalizeStartedAt)
	traceFile(config.Trace, "normalized-wav", transcriptionInputPath)

	fmt.Println("==> ensuring whisper model")
	modelStartedAt := time.Now()
	modelDir, err := defaultModelDir()
	if err != nil {
		return Result{}, err
	}
	modelPath, err := whisperClient.EnsureModel(ctx, modelDir, config.ModelName)
	if err != nil {
		return Result{}, err
	}
	traceStage(config.Trace, "ensure whisper model", modelStartedAt)
	traceFile(config.Trace, "whisper-model", modelPath)

	fmt.Printf("==> transcribing audio via %s\n", config.Backend)
	transcribeStartedAt := time.Now()
	var rawTranscriptPath string
	var transcript whisper.Output
	if config.Backend == "ffmpeg-whisper" {
		rawTranscriptPath, transcript, err = whisperClient.TranscribeWithFFmpeg(ctx, transcriptionInputPath, tempDir, workspaceDir, modelPath, config.Language, config.UseGPU, config.GPUDevice, config.FFmpegWhisperQueueSeconds, metadata.Duration, config.Force)
	} else {
		rawTranscriptPath, transcript, err = whisperClient.Transcribe(ctx, transcriptionInputPath, tempDir, workspaceDir, modelPath, config.Language, config.UseGPU, config.Force)
	}
	if err != nil {
		return Result{}, err
	}
	if len(transcript.Transcription) == 0 {
		return Result{}, fmt.Errorf("whisper produced no transcription segments")
	}
	traceStage(config.Trace, "transcribe audio", transcribeStartedAt)
	traceFile(config.Trace, "raw-transcript", rawTranscriptPath)
	if config.Trace {
		fmt.Printf("[trace] transcript backend=%q gpu=%t gpu-device=%d segments=%d detected-language=%q covered=%s\n", config.Backend, config.UseGPU, config.GPUDevice, len(transcript.Transcription), transcript.Result.Language, transcriptDuration(transcript))
	}

	fmt.Println("==> rendering markdown")
	renderStartedAt := time.Now()
	rawTranscriptReference := ""
	if config.KeepArtifacts {
		rawTranscriptReference = filepath.Base(rawTranscriptPath)
	}
	markdown := render.Markdown(metadata, transcript, render.Options{
		ModelName:         config.ModelName,
		RequestedLanguage: config.Language,
		DetectedLanguage:  transcript.Result.Language,
		GeneratedAt:       time.Now(),
		ChunkSeconds:      config.ChunkSeconds,
		SourceURL:         config.URL,
		RawTranscriptPath: rawTranscriptReference,
	})
	tempMarkdownPath := filepath.Join(tempDir, "transcript.md")
	markdownPath := filepath.Join(workspaceDir, "transcript.md")
	if err := shell.WriteText(tempMarkdownPath, markdown); err != nil {
		return Result{}, fmt.Errorf("write temp markdown transcript: %w", err)
	}
	if err := shell.WriteText(markdownPath, markdown); err != nil {
		return Result{}, fmt.Errorf("write markdown transcript: %w", err)
	}
	traceStage(config.Trace, "render markdown", renderStartedAt)
	traceFile(config.Trace, "markdown", markdownPath)

	if !config.KeepArtifacts {
		cleanupStartedAt := time.Now()
		if err := pruneWorkspace(workspaceDir, markdownPath); err != nil {
			return Result{}, err
		}
		traceStage(config.Trace, "prune workspace", cleanupStartedAt)
		tempDir = ""
		rawTranscriptPath = ""
	}
	traceStage(config.Trace, "total pipeline", totalStartedAt)

	return Result{
		Metadata:          metadata,
		WorkspaceDir:      workspaceDir,
		TempDir:           tempDir,
		MarkdownPath:      markdownPath,
		RawTranscriptPath: rawTranscriptPath,
	}, nil
}

func traceStage(enabled bool, name string, startedAt time.Time) {
	if !enabled {
		return
	}
	fmt.Printf("[trace] stage=%q elapsed=%s\n", name, time.Since(startedAt).Round(time.Millisecond))
}

func traceFile(enabled bool, label string, path string) {
	if !enabled {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		fmt.Printf("[trace] file=%q path=%q stat-error=%v\n", label, path, err)
		return
	}
	fmt.Printf("[trace] file=%q path=%q size=%s\n", label, path, formatBytes(info.Size()))
}

func transcriptDuration(output whisper.Output) string {
	if len(output.Transcription) == 0 {
		return "0s"
	}
	return time.Duration(output.Transcription[len(output.Transcription)-1].Offsets.To * int(time.Millisecond)).String()
}

func formatSeconds(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func defaultModelDir() (string, error) {
	return whisper.ModelDir()
}

func pruneWorkspace(workspaceDir string, markdownPath string) error {
	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		return fmt.Errorf("read workspace dir: %w", err)
	}
	keepName := filepath.Base(markdownPath)
	for _, entry := range entries {
		if entry.Name() == keepName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(workspaceDir, entry.Name())); err != nil {
			return fmt.Errorf("remove workspace artifact %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func workspaceDir(outputRoot string, metadata source.Metadata) (string, error) {
	root, err := filepath.Abs(outputRoot)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	slug := slugify(metadata.Title)
	if slug == "" {
		slug = metadata.ID
	}
	name := metadata.ID
	if slug != "" {
		name = fmt.Sprintf("%s-%s", metadata.ID, slug)
	}
	return filepath.Join(root, name), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 64 {
		value = strings.Trim(value[:64], "-")
	}
	return value
}
