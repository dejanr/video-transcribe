package subtitle

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
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
	input, err := resolveCapture(ctx, config)
	if err != nil {
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
	session := session{config: config, server: server, detector: detector, events: events}
	return session.listen(ctx, input, controls)
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
