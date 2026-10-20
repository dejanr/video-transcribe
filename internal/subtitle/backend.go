package subtitle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

type serverCandidate struct {
	name       string
	program    string
	noGPU      bool
	requireGPU bool
}

func serverCandidates(noGPU bool) []serverCandidate {
	cpu := os.Getenv("AUDIO_SUBTITLE_WHISPER_CPU")
	if cpu == "" {
		cpu = "whisper-server"
	}
	if noGPU {
		return []serverCandidate{{name: "CPU", program: cpu, noGPU: true}}
	}
	var candidates []serverCandidate
	for _, backend := range []struct{ name, variable string }{
		{"ROCm", "AUDIO_SUBTITLE_WHISPER_ROCM"},
		{"Vulkan", "AUDIO_SUBTITLE_WHISPER_VULKAN"},
	} {
		if program := os.Getenv(backend.variable); program != "" {
			candidates = append(candidates, serverCandidate{name: backend.name, program: program, requireGPU: true})
		}
	}
	if len(candidates) == 0 {
		candidates = append(candidates, serverCandidate{
			name: "Whisper", program: "whisper-server", requireGPU: cpu != "whisper-server",
		})
	}
	return append(candidates, serverCandidate{name: "CPU", program: cpu, noGPU: true})
}

func startServer(ctx context.Context, config Config, modelPath string) (*inferenceServer, func(), error) {
	var failures []error
	candidates := serverCandidates(config.NoGPU)
	for index, candidate := range candidates {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		server, stop, err := startServerCandidate(ctx, config, modelPath, candidate)
		if err == nil {
			return server, stop, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", candidate.name, err))
		if ctx.Err() == nil && index+1 < len(candidates) {
			fmt.Fprintf(os.Stderr, "[runtime] %s unavailable; trying %s: %v\n", candidate.name, candidates[index+1].name, err)
		}
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	return nil, nil, fmt.Errorf("no usable inference backend: %w", errors.Join(failures...))
}

type backendLog struct {
	mu      sync.Mutex
	pending string
	backend string
}

func (l *backendLog) Write(body []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending += string(body)
	for {
		line, rest, found := strings.Cut(l.pending, "\n")
		if !found {
			break
		}
		l.pending = rest
		if _, name, found := strings.Cut(line, "whisper_backend_init_gpu: using "); found {
			l.backend = strings.TrimSuffix(strings.TrimSpace(name), " backend")
		}
	}
	if len(l.pending) > 4096 {
		l.pending = ""
	}
	return os.Stderr.Write(body)
}

func (l *backendLog) name() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.backend == "" {
		return "CPU"
	}
	return l.backend
}
