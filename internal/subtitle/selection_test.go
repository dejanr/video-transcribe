package subtitle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fakeServers(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"whisper-server", "gpu-server", "gpu-crash", "gpu-none", "gpu-bad-inference", "gpu-hang", "rocm-server", "rocm-crash", "rocm-none", "rocm-bad-inference", "rocm-hang"} {
		script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=TestSubtitleHelperProcess -- %s \"$@\"\n", strconv.Quote(executable), name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AUDIO_SUBTITLE_WHISPER_ROCM", "")
	t.Setenv("SUBTITLE_HELPER_PROCESS", "1")
	t.Setenv("SUBTITLE_HELPER_DIR", dir)
	t.Setenv("AUDIO_SUBTITLE_WHISPER_CPU", filepath.Join(dir, "whisper-server"))
	t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", filepath.Join(dir, "gpu-server"))
	return dir
}

func TestStartupSelectsUsableGPUOrFallsBackToCPU(t *testing.T) {
	for _, test := range []struct {
		gpu   string
		noGPU bool
		want  string
	}{
		{"gpu-server", false, "Vulkan0"},
		{"gpu-crash", false, "CPU"},
		{"gpu-none", false, "CPU"},
		{"gpu-bad-inference", false, "CPU"},
		{"missing", false, "CPU"},
		{"gpu-server", true, "CPU"},
	} {
		t.Run(fmt.Sprintf("%s/noGPU=%t", test.gpu, test.noGPU), func(t *testing.T) {
			dir := fakeServers(t)
			t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", filepath.Join(dir, test.gpu))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server, stop, err := startServer(ctx, Config{NoGPU: test.noGPU, Threads: 1}, "fixture.bin")
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if server.backend != test.want {
				t.Fatalf("selected %q, want %q", server.backend, test.want)
			}
			if test.want == "CPU" {
				body, err := os.ReadFile(filepath.Join(dir, "whisper-server.args"))
				if err != nil || !strings.Contains(string(body), "-ng") {
					t.Fatalf("CPU fallback was not explicitly forced: %s, %v", body, err)
				}
			} else {
				if _, err := os.Stat(filepath.Join(dir, "inference-called")); err != nil {
					t.Fatal("GPU was selected without testing inference")
				}
				if _, err := os.Stat(filepath.Join(dir, "whisper-server.args")); !os.IsNotExist(err) {
					t.Fatal("CPU process started unnecessarily")
				}
			}
			if test.noGPU {
				if _, err := os.Stat(filepath.Join(dir, "gpu-server.args")); !os.IsNotExist(err) {
					t.Fatal("--no-gpu still started the GPU process")
				}
			}
		})
	}
}

func TestStartupPrefersROCmThenVulkanThenCPU(t *testing.T) {
	for _, test := range []struct {
		name   string
		rocm   string
		vulkan string
		noGPU  bool
		want   string
	}{
		{"rocm preferred", "rocm-server", "gpu-server", false, "ROCm0"},
		{"rocm crash", "rocm-crash", "gpu-server", false, "Vulkan0"},
		{"rocm missing", "missing-rocm", "gpu-server", false, "Vulkan0"},
		{"rocm has no device", "rocm-none", "gpu-server", false, "Vulkan0"},
		{"rocm inference fails", "rocm-bad-inference", "gpu-server", false, "Vulkan0"},
		{"both GPUs fail", "rocm-crash", "gpu-bad-inference", false, "CPU"},
		{"CPU override", "rocm-server", "gpu-server", true, "CPU"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := fakeServers(t)
			t.Setenv("AUDIO_SUBTITLE_WHISPER_ROCM", filepath.Join(dir, test.rocm))
			t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", filepath.Join(dir, test.vulkan))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server, stop, err := startServer(ctx, Config{Threads: 1, NoGPU: test.noGPU}, "fixture.bin")
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if server.backend != test.want {
				t.Fatalf("selected %s, want %s", server.backend, test.want)
			}
			if test.want == "ROCm0" || test.noGPU {
				if _, err := os.Stat(filepath.Join(dir, test.vulkan+".args")); !os.IsNotExist(err) {
					t.Fatal("Vulkan was started despite an earlier successful backend or --no-gpu")
				}
			}
			if test.want != "CPU" {
				if _, err := os.Stat(filepath.Join(dir, "whisper-server.args")); !os.IsNotExist(err) {
					t.Fatal("CPU was started despite a successful GPU")
				}
			}
			if test.noGPU {
				if _, err := os.Stat(filepath.Join(dir, test.rocm+".args")); !os.IsNotExist(err) {
					t.Fatal("--no-gpu still started ROCm")
				}
			}
		})
	}
}

func TestBackendSelectionStopsBeforeVulkanOnROCmCancellation(t *testing.T) {
	dir := fakeServers(t)
	t.Setenv("AUDIO_SUBTITLE_WHISPER_ROCM", filepath.Join(dir, "rocm-hang"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := startServer(ctx, Config{Threads: 1}, "fixture.bin")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selection did not propagate cancellation: %v", err)
	}
	for _, name := range []string{"gpu-server.args", "whisper-server.args"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("selection continued after cancellation")
		}
	}
}

func TestBackendSelectionStopsOnCancellation(t *testing.T) {
	dir := fakeServers(t)
	t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", filepath.Join(dir, "gpu-hang"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := startServer(ctx, Config{Threads: 1}, "fixture.bin")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selection did not propagate cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "whisper-server.args")); !os.IsNotExist(err) {
		t.Fatal("selection retried on CPU after cancellation")
	}
}

func TestBackendSelectionReportsFailureOfAllRuntimes(t *testing.T) {
	dir := fakeServers(t)
	t.Setenv("AUDIO_SUBTITLE_WHISPER_ROCM", filepath.Join(dir, "rocm-crash"))
	t.Setenv("AUDIO_SUBTITLE_WHISPER_VULKAN", filepath.Join(dir, "gpu-crash"))
	t.Setenv("AUDIO_SUBTITLE_WHISPER_CPU", filepath.Join(dir, "missing-cpu"))
	_, _, err := startServer(context.Background(), Config{Threads: 1}, "fixture.bin")
	if err == nil || !strings.Contains(err.Error(), "no usable inference backend") {
		t.Fatalf("expected a combined startup error, got %v", err)
	}
	for _, name := range []string{"ROCm", "Vulkan", "CPU"} {
		if !strings.Contains(err.Error(), name+":") {
			t.Fatalf("startup error omitted %s: %v", name, err)
		}
	}
}
