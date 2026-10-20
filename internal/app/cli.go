package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dejanr/video-transcribe/internal/pipeline"
)

func Run(argv []string) error {
	programName, args := parseInvocation(argv)
	usage := usage(programName)
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	if args[0] == "transcribe" {
		return fmt.Errorf("'transcribe' subcommand was removed; use %s [flags] <source>\n\n%s", programName, usage)
	}
	return transcribeCommand(programName, args, usage)
}

func parseInvocation(argv []string) (string, []string) {
	if len(argv) == 0 {
		return "video-transcribe", nil
	}
	programName := strings.TrimSpace(filepath.Base(argv[0]))
	if programName == "" || programName == "." || programName == string(filepath.Separator) {
		programName = "video-transcribe"
	}
	return programName, argv[1:]
}

func usage(programName string) string {
	return fmt.Sprintf(`%s transcribes audio from a supported video URL or local media file
and renders a markdown transcript for later summarization.

Usage:
  %s [flags] <source>

Source can be a video URL, a local file path, or a file:// URL.

Flags:
  -output-dir string
        Directory where artifacts are written. (default "./output")
  -model string
        whisper.cpp model to use. (default "large-v3-turbo")
  -backend string
        Transcription backend: whisper-cli or ffmpeg-whisper. (default "whisper-cli")
  -ffmpeg-whisper-queue-seconds int
        Queue size in seconds for the ffmpeg whisper filter. Higher values trade latency for throughput. (default 15)
  -language string
        Spoken language for whisper.cpp, or auto to detect. (default "en")
  -no-gpu
        Disable GPU acceleration even when the selected backend was built with GPU support.
  -gpu-device int
        GPU device index for the ffmpeg whisper backend. (default 0)
  -keep
        Keep downloaded and intermediate artifacts instead of only transcript.md.
  -force
        Re-run every stage even if artifacts already exist.
  -chunk-seconds int
        Target markdown chunk size in seconds. (default 45)
  -trace
        Print stage timings and external command timings.
`, programName, programName)
}

func transcribeCommand(programName string, args []string, usage string) error {
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := pipeline.Config{UseGPU: true}
	var noGPU bool
	fs.StringVar(&cfg.OutputDir, "output-dir", "./output", "Directory where artifacts are written.")
	fs.StringVar(&cfg.ModelName, "model", "large-v3-turbo", "whisper.cpp model to use.")
	fs.StringVar(&cfg.Backend, "backend", "whisper-cli", "Transcription backend: whisper-cli or ffmpeg-whisper.")
	fs.IntVar(&cfg.FFmpegWhisperQueueSeconds, "ffmpeg-whisper-queue-seconds", 15, "Queue size in seconds for the ffmpeg whisper filter. Higher values trade latency for throughput.")
	fs.StringVar(&cfg.Language, "language", "en", "Spoken language for whisper.cpp, or auto to detect.")
	fs.BoolVar(&noGPU, "no-gpu", false, "Disable GPU acceleration even when the selected backend was built with GPU support.")
	fs.IntVar(&cfg.GPUDevice, "gpu-device", 0, "GPU device index for the ffmpeg whisper backend.")
	fs.BoolVar(&cfg.KeepArtifacts, "keep", false, "Keep downloaded and intermediate artifacts instead of only transcript.md.")
	fs.BoolVar(&cfg.Force, "force", false, "Re-run every stage even if artifacts already exist.")
	fs.IntVar(&cfg.ChunkSeconds, "chunk-seconds", 45, "Target markdown chunk size in seconds.")
	fs.BoolVar(&cfg.Trace, "trace", false, "Print stage timings and external command timings.")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one source URL or local file\n\n%s", usage)
	}

	cfg.UseGPU = !noGPU
	cfg.URL = fs.Arg(0)
	result, err := pipeline.Run(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("done\n")
	fmt.Printf("title: %s\n", result.Metadata.Title)
	fmt.Printf("workspace: %s\n", result.WorkspaceDir)
	fmt.Printf("markdown: %s\n", result.MarkdownPath)
	if result.TempDir != "" {
		fmt.Printf("tmp: %s\n", result.TempDir)
	}
	if result.RawTranscriptPath != "" {
		fmt.Printf("raw transcript: %s\n", result.RawTranscriptPath)
	}
	return nil
}
