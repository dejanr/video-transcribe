package subtitle

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dejanr/video-transcribe/internal/shell"
	"github.com/dejanr/video-transcribe/internal/whisper"
)

type Event struct {
	Type             string  `json:"type"`
	Text             string  `json:"text"`
	LatencySeconds   float64 `json:"latency_seconds,omitempty"`
	ID               uint64  `json:"id,omitempty"`
	AudioSeconds     float64 `json:"audio_seconds,omitempty"`
	InferenceSeconds float64 `json:"inference_seconds,omitempty"`
	QueueSeconds     float64 `json:"queue_seconds,omitempty"`
	VADSeconds       float64 `json:"vad_seconds,omitempty"`
	Runtime          string  `json:"runtime,omitempty"`
}

type emitter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (e *emitter) send(event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.encoder.Encode(event)
}

type clip struct {
	pcm        []byte
	id         uint64
	final      bool
	overlapped bool
	started    time.Time
	queued     time.Time
	generation uint64
}

type playback struct {
	paused     atomic.Bool
	generation atomic.Uint64
}

func (p *playback) accepts(audio clip) bool {
	return !p.paused.Load() && audio.generation == p.generation.Load()
}

func (p *playback) commands(input io.Reader) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		switch strings.TrimSpace(scanner.Text()) {
		case "pause":
			p.paused.Store(true)
			p.generation.Add(1)
		case "resume":
			p.generation.Add(1)
			p.paused.Store(false)
		}
	}
}

func Run(ctx context.Context, config Config, controls io.Reader, output io.Writer) error {
	if err := config.Validate(); err != nil {
		return err
	}
	events := &emitter{encoder: json.NewEncoder(output)}
	modelPath, detector, err := prepareModels(ctx, config, events)
	if err != nil {
		return err
	}
	events.send(Event{Type: "status", Text: "Selecting and loading the best available inference backend…"})
	server, stopServer, err := startServer(ctx, config, modelPath)
	if err != nil {
		return err
	}
	defer stopServer()
	source, err := outputMonitor(ctx, config.Source)
	if err != nil {
		return err
	}
	session := session{config: config, server: server, detector: detector, events: events}
	return session.listen(ctx, source, controls)
}

func prepareModels(ctx context.Context, config Config, events *emitter) (string, speechDetector, error) {
	detector := speechDetector{threshold: config.VADThreshold, modelPath: config.VADModelPath}
	events.send(Event{Type: "status", Text: "Preparing local translation model (first run may download it)…"})
	modelDir, err := whisper.ModelDir()
	if err != nil {
		return "", detector, err
	}
	client := whisper.Client{Runner: shell.Runner{Stdout: os.Stderr, KillProcessGroup: true}}
	modelPath, err := client.EnsureModel(ctx, modelDir, config.Model)
	if err != nil {
		return "", detector, err
	}
	if !config.NoVAD && detector.modelPath == "" {
		events.send(Event{Type: "status", Text: "Preparing Silero voice activity detection…"})
		detector.modelPath, err = client.EnsureVADModel(ctx, modelDir)
	}
	return modelPath, detector, err
}

func outputMonitor(ctx context.Context, source string) (string, error) {
	if source != "" {
		return source, nil
	}
	sink, err := (shell.Runner{}).Capture(ctx, "pactl", "get-default-sink")
	if err != nil {
		return "", fmt.Errorf("find audio output (requires PulseAudio or PipeWire-Pulse): %w", err)
	}
	return strings.TrimSpace(string(sink)) + ".monitor", nil
}

func capture(ctx context.Context, config Config, source string, state *playback, clips chan clip, events *emitter) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-f", "pulse", "-i", source,
		"-ac", "1", "-ar", "16000", "-f", "s16le", "pipe:1",
	)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("capture output audio: %w", err)
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
			return fmt.Errorf("audio capture stopped: %v (ffmpeg: %v)", readErr, waitErr)
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

func enqueueLatest(queue chan clip, audio clip) bool {
	select {
	case queue <- audio:
		return false
	default:
	}
	dropped := false
	select {
	case <-queue:
		dropped = true
	default:
	}
	queue <- audio
	return dropped
}
