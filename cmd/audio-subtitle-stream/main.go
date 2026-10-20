package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dejanr/video-transcribe/internal/shell"
	"github.com/dejanr/video-transcribe/internal/subtitle"
)

func main() {
	config := subtitle.Config{}
	flag.StringVar(&config.Model, "model", "small", "Multilingual Whisper model (tiny, base, small, medium, large-v3).")
	flag.StringVar(&config.Source, "source", "", "PulseAudio monitor source; defaults to the current output's monitor.")
	flag.Float64Var(&config.ChunkSeconds, "chunk-seconds", 6, "Maximum audio context window, 2–15 seconds.")
	flag.Float64Var(&config.StepSeconds, "step-seconds", 1.5, "Preview interval in seconds; 0 disables previews.")
	flag.Float64Var(&config.OverlapSeconds, "overlap-seconds", 0.75, "Audio carried across window boundaries, 0–1 seconds.")
	flag.StringVar(&config.VADModelPath, "vad-model", "", "Custom Silero GGML model path; defaults to the cached, verified model.")
	flag.BoolVar(&config.NoVAD, "no-vad", false, "Disable Silero VAD (keep the lightweight energy gate).")
	flag.Float64Var(&config.VADThreshold, "vad-threshold", 0.5, "Silero speech probability threshold, between 0 and 1.")
	flag.BoolVar(&config.FullContext, "full-context", false, "Use the full Whisper encoder context (slower, potentially more accurate).")
	flag.Float64Var(&config.Threshold, "silence-threshold", 0.008, "RMS speech gate, between 0 and 1; lower for quiet audio.")
	flag.IntVar(&config.Threads, "threads", 4, "Whisper CPU threads.")
	flag.BoolVar(&config.NoGPU, "no-gpu", false, "Disable GPU inference.")
	listSources := flag.Bool("list-sources", false, "List PulseAudio sources (choose an output monitor).")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if flag.NArg() != 0 {
		err = fmt.Errorf("unexpected positional arguments")
	} else if *listSources {
		err = (shell.Runner{}).Run(ctx, "pactl", "list", "short", "sources")
	} else {
		err = subtitle.Run(ctx, config, os.Stdin, os.Stdout)
	}
	if err != nil && ctx.Err() == nil {
		_ = json.NewEncoder(os.Stdout).Encode(subtitle.Event{Type: "error", Text: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
