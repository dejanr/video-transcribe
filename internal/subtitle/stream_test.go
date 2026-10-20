package subtitle

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	valid := Config{Model: "small", ChunkSeconds: 6, Threshold: 0.008, VADThreshold: 0.5, Threads: 4}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"small.en", "large-v3-turbo", "../model", "", "foo/bar"} {
		config := valid
		config.Model = model
		if config.Validate() == nil {
			t.Fatalf("accepted unsuitable model %q", model)
		}
	}
	for _, seconds := range []float64{0, 1, 16} {
		config := valid
		config.ChunkSeconds = seconds
		if config.Validate() == nil {
			t.Fatalf("accepted chunk length %v", seconds)
		}
	}
}

func TestPreviewIntervalValidation(t *testing.T) {
	for _, step := range []float64{0, 0.5, 1.5, 6} {
		config := Config{Model: "small", ChunkSeconds: 6, StepSeconds: step, Threshold: 0.008, VADThreshold: 0.5, Threads: 4}
		if err := config.Validate(); err != nil {
			t.Fatalf("rejected valid step %v: %v", step, err)
		}
	}
	for _, step := range []float64{-1, 0.1, 7, math.NaN(), math.Inf(1)} {
		config := Config{Model: "small", ChunkSeconds: 6, StepSeconds: step, Threshold: 0.008, VADThreshold: 0.5, Threads: 4}
		if config.Validate() == nil {
			t.Fatalf("accepted invalid step %v", step)
		}
	}
}

func TestOverlapAndVADValidation(t *testing.T) {
	valid := Config{Model: "base", ChunkSeconds: 6, StepSeconds: 1.5, Threshold: 0.008, VADThreshold: 0.5, Threads: 4}
	for _, overlap := range []float64{-0.25, 1.25, math.NaN(), math.Inf(1)} {
		config := valid
		config.OverlapSeconds = overlap
		if config.Validate() == nil {
			t.Fatalf("accepted invalid overlap %v", overlap)
		}
	}
	for _, threshold := range []float64{0, 1, -0.1, math.NaN()} {
		config := valid
		config.VADThreshold = threshold
		if config.Validate() == nil {
			t.Fatalf("accepted invalid VAD threshold %v", threshold)
		}
	}
	valid.OverlapSeconds = 0.75
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.NoVAD = true
	valid.VADThreshold = 0
	if err := valid.Validate(); err != nil {
		t.Fatal("disabled VAD should not require a threshold")
	}
}

func TestQueueDropsOldAudioInsteadOfGrowing(t *testing.T) {
	queue := make(chan clip, 2)
	if enqueueLatest(queue, clip{generation: 1}) || enqueueLatest(queue, clip{generation: 2}) {
		t.Fatal("dropped audio before queue filled")
	}
	if !enqueueLatest(queue, clip{generation: 3}) {
		t.Fatal("did not report skipped audio")
	}
	if (<-queue).generation != 2 || (<-queue).generation != 3 {
		t.Fatal("did not retain the newest audio")
	}
}

func TestPauseCommandsInvalidatePendingCaptions(t *testing.T) {
	state := playback{}
	state.commands(strings.NewReader("pause\n"))
	if !state.paused.Load() || state.generation.Load() != 1 {
		t.Fatal("pause did not invalidate pending captions")
	}
	state.commands(strings.NewReader("resume\n"))
	if state.paused.Load() || state.generation.Load() != 2 {
		t.Fatal("resume did not begin a fresh generation")
	}
}

func TestRunWithLocalProcesses(t *testing.T) {
	testRunWithLocalProcesses(t, true)
}

func TestRunSkipsWhisperForNonSpeech(t *testing.T) {
	testRunWithLocalProcesses(t, false)
}

func testRunWithLocalProcesses(t *testing.T, speech bool) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AUDIO_SUBTITLE_WHISPER_ROCM", "")
	t.Setenv("AUDIO_SUBTITLE_WHISPER_CPU", "")
	t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", "")
	t.Setenv("AUDIO_SUBTITLE_VAD", "")
	t.Setenv("VIDEO_TRANSCRIBE_MODEL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "ggml-small.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"whisper-server", "whisper-vad-speech-segments", "ffmpeg", "pactl"} {
		script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=TestSubtitleHelperProcess -- %s \"$@\"\n", strconv.Quote(executable), name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUBTITLE_HELPER_PROCESS", "1")
	t.Setenv("SUBTITLE_HELPER_DIR", dir)
	t.Setenv("SUBTITLE_HELPER_SPEECH", strconv.FormatBool(speech))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := Run(ctx, Config{Model: "small", ChunkSeconds: 2, StepSeconds: 1, Threshold: 0.008, VADThreshold: 0.5, VADModelPath: filepath.Join(dir, "ggml-small.bin"), Threads: 1}, strings.NewReader(""), writer)
		writer.Close()
		done <- err
	}()
	found := false
	previewID := uint64(0)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("stdout was not NDJSON: %s", scanner.Text())
		}
		if event.Type == "partial" {
			previewID = event.ID
			if event.AudioSeconds < 1 || event.InferenceSeconds <= 0 {
				t.Errorf("preview lacks timing information: %+v", event)
			}
		}
		if event.Type == "subtitle" {
			if speech && (previewID == 0 || event.ID != previewID) {
				t.Error("final caption did not revise the earlier preview")
			}
			expected := ""
			if speech {
				expected = "Good evening, everyone."
			}
			if event.Text != expected {
				t.Errorf("unexpected subtitle %q", event.Text)
			}
			found = true
			cancel()
		}
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected clean cancellation, got %v", err)
	}
	if !found {
		t.Fatal("no completed window arrived from capture → VAD → NDJSON")
	}
	_, inferenceErr := os.Stat(filepath.Join(dir, "inference-called"))
	if speech != (inferenceErr == nil) {
		t.Fatalf("Whisper inference ran despite speech=%t (stat: %v)", speech, inferenceErr)
	}
}

func TestSubtitleHelperProcess(t *testing.T) {
	if os.Getenv("SUBTITLE_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	switch args[0] {
	case "pactl":
		fmt.Println("test-output")
	case "ffmpeg":
		for range 4 {
			os.Stdout.Write(audioFrame(1000))
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(os.Getenv("SUBTITLE_HELPER_DIR"), "vad-called")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(2)
			}
			time.Sleep(5 * time.Millisecond)
		}
		for range 2 {
			os.Stdout.Write(audioFrame(1000))
		}
		for range 2 {
			os.Stdout.Write(audioFrame(0))
		}
		time.Sleep(time.Minute)
	case "whisper-vad-speech-segments":
		body, _ := io.ReadAll(os.Stdin)
		if len(body) < 44 || string(body[:4]) != "RIFF" {
			os.Exit(2)
		}
		count := 0
		if os.Getenv("SUBTITLE_HELPER_SPEECH") == "true" {
			count = 1
		}
		fmt.Printf("Detected %d speech segments:\n", count)
		_ = os.WriteFile(filepath.Join(os.Getenv("SUBTITLE_HELPER_DIR"), "vad-called"), nil, 0o600)
	case "whisper-server", "gpu-server", "gpu-crash", "gpu-none", "gpu-bad-inference", "gpu-hang",
		"rocm-server", "rocm-crash", "rocm-none", "rocm-bad-inference", "rocm-hang":
		_ = os.WriteFile(filepath.Join(os.Getenv("SUBTITLE_HELPER_DIR"), args[0]+".args"), []byte(strings.Join(args[1:], " ")), 0o600)
		if strings.HasSuffix(args[0], "-crash") {
			os.Exit(2)
		}
		if args[0] != "whisper-server" && !strings.HasSuffix(args[0], "-none") {
			device := "Vulkan0"
			if strings.HasPrefix(args[0], "rocm-") {
				device = "ROCm0"
			}
			fmt.Fprintf(os.Stderr, "whisper_backend_init_gpu: using %s backend\n", device)
		}
		mode := args[0]
		port := ""
		for i, arg := range args {
			if arg == "--port" {
				port = args[i+1]
			}
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"status":"ok"}`)
		})
		mux.HandleFunc("/inference", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(mode, "-bad-inference") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if strings.HasSuffix(mode, "-hang") {
				<-r.Context().Done()
				return
			}
			io.WriteString(w, `{"text":"Good evening, everyone."}`)
			_ = os.WriteFile(filepath.Join(os.Getenv("SUBTITLE_HELPER_DIR"), "inference-called"), nil, 0o600)
		})
		if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
