package subtitle

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dejanr/video-transcribe/internal/shell"
)

const macCaptureProgram = "audio-subtitle-mac-capture"

type captureInput struct {
	label          string
	program        string
	args           []string
	startupTimeout time.Duration
}

func ListSources(ctx context.Context) error {
	runner := shell.Runner{}
	if runtime.GOOS == "darwin" {
		return runner.Run(ctx, macCaptureProgram, "--list-sources")
	}
	return runner.Run(ctx, "pactl", "list", "short", "sources")
}

func resolveCapture(ctx context.Context, config Config) (captureInput, error) {
	if config.Mic {
		return microphoneInput(ctx)
	}
	source := config.Source
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath(macCaptureProgram); err != nil {
			return captureInput{}, fmt.Errorf("find the macOS capture helper (use the Nix subtitle app): %w", err)
		}
		input := captureInput{label: "macOS system audio", program: macCaptureProgram}
		if source != "" {
			displayID, err := strconv.ParseUint(source, 10, 32)
			if err != nil {
				return captureInput{}, fmt.Errorf("macOS source must be a display ID; see --list-sources")
			}
			input.args = []string{strconv.FormatUint(displayID, 10)}
			input.label = "macOS system audio · display " + input.args[0]
		}
		return input, nil
	}
	if source == "" {
		sink, err := (shell.Runner{}).Capture(ctx, "pactl", "get-default-sink")
		if err != nil {
			return captureInput{}, fmt.Errorf("find audio output (requires PulseAudio or PipeWire-Pulse): %w", err)
		}
		source = strings.TrimSpace(string(sink)) + ".monitor"
	}
	return ffmpegCapture("pulse", source, source), nil
}

func microphoneInput(ctx context.Context) (captureInput, error) {
	if runtime.GOOS == "darwin" {
		input := ffmpegCapture("avfoundation", "none:default", "Default microphone")
		input.startupTimeout = 30 * time.Second
		return input, nil
	}
	value, err := (shell.Runner{}).Capture(ctx, "pactl", "get-default-source")
	if err != nil {
		return captureInput{}, fmt.Errorf("find default microphone (requires PulseAudio or PipeWire-Pulse): %w", err)
	}
	source := strings.TrimSpace(string(value))
	if source == "" || strings.HasSuffix(source, ".monitor") {
		return captureInput{}, fmt.Errorf("set a microphone as the default input in sound settings, or choose it with --source")
	}
	return ffmpegCapture("pulse", source, "Microphone · "+source), nil
}

func ffmpegCapture(format, source, label string) captureInput {
	return captureInput{label: label, program: "ffmpeg", args: []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-f", format, "-i", source, "-vn",
		"-ac", "1", "-ar", strconv.Itoa(sampleRate), "-f", "s16le", "pipe:1",
	}}
}

func capture(ctx context.Context, config Config, input captureInput, state *playback, clips chan clip, events *emitter) error {
	captureCtx, cancelCapture := context.WithCancel(ctx)
	defer cancelCapture()
	cmd := exec.CommandContext(captureCtx, input.program, input.args...)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s audio capture: %w", input.program, err)
	}
	var firstFrame *time.Timer
	if input.startupTimeout > 0 {
		firstFrame = time.AfterFunc(input.startupTimeout, cancelCapture)
		defer firstFrame.Stop()
	}
	chunks := chunker{
		threshold:     config.Threshold,
		maxFrames:     int(math.Ceil(config.ChunkSeconds * 4)),
		stepFrames:    int(math.Ceil(config.StepSeconds * 4)),
		overlapFrames: int(math.Ceil(config.OverlapSeconds * 4)),
	}
	frame := make([]byte, frameBytes)
	generation := state.generation.Load()
	for {
		_, readErr := io.ReadFull(pipe, frame)
		if readErr != nil {
			waitErr := cmd.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if captureCtx.Err() != nil {
				return fmt.Errorf("microphone did not start; check System Settings → Privacy & Security → Microphone and Sound → Input, then relaunch")
			}
			return fmt.Errorf("audio capture stopped (%s: %v): %w; see terminal for details", input.program, waitErr, readErr)
		}
		if firstFrame != nil {
			firstFrame.Stop()
			firstFrame = nil
		}
		current := state.generation.Load()
		if current != generation || state.paused.Load() {
			chunks.reset()
			generation = current
		}
		if state.paused.Load() {
			continue
		}
		chunk := chunks.push(frame)
		if chunk == nil {
			continue
		}
		audio := clip{
			pcm: chunk.pcm, id: chunk.id, final: chunk.final, overlapped: chunk.overlapped,
			started: time.Now().Add(-time.Duration(float64(len(chunk.pcm)) / (sampleRate * 2) * float64(time.Second))), queued: time.Now(), generation: generation,
		}
		if enqueueLatest(clips, audio) {
			events.send(Event{Type: "status", Text: "Inference is slower than playback · skipped old audio to stay near live. Try a smaller model or GPU build."})
		}
	}
}
