package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type inferenceServer struct {
	url         string
	client      *http.Client
	done        chan struct{}
	err         error
	fullContext bool
	backend     string
}

func startServerCandidate(ctx context.Context, config Config, modelPath string, candidate serverCandidate) (*inferenceServer, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	workDir, err := os.MkdirTemp("", "audio-subtitle-")
	if err != nil {
		return nil, nil, err
	}
	serverCtx, cancel := context.WithCancel(ctx)
	args := []string{
		"-m", modelPath, "-l", "de", "-tr", "-sns",
		"--host", "127.0.0.1", "--port", strconv.Itoa(port),
		"--public", workDir, "--tmp-dir", workDir,
		"-t", strconv.Itoa(config.Threads),
	}
	if candidate.noGPU {
		args = append(args, "-ng")
	}
	cmd := exec.CommandContext(serverCtx, candidate.program, args...)
	logs := &backendLog{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(workDir)
		return nil, nil, fmt.Errorf("start whisper-server: %w", err)
	}
	server := &inferenceServer{
		url: fmt.Sprintf("http://127.0.0.1:%d", port),
		client: &http.Client{
			Timeout:   120 * time.Second,
			Transport: &http.Transport{Proxy: nil},
		},
		done:        make(chan struct{}),
		fullContext: config.FullContext,
	}
	go func() {
		server.err = cmd.Wait()
		close(server.done)
	}()
	stop := func() {
		cancel()
		<-server.done
		server.client.CloseIdleConnections()
		os.RemoveAll(workDir)
	}
	readyCtx := ctx
	if candidate.requireGPU {
		var readyCancel context.CancelFunc
		readyCtx, readyCancel = context.WithTimeout(ctx, 30*time.Second)
		defer readyCancel()
	}
	if err := server.waitReady(readyCtx); err != nil {
		stop()
		return nil, nil, err
	}
	server.backend = logs.name()
	if candidate.requireGPU {
		if server.backend == "CPU" {
			stop()
			return nil, nil, fmt.Errorf("no GPU selected by %s", candidate.program)
		}
		if _, err := server.translate(readyCtx, make([]byte, sampleRate*2)); err != nil {
			stop()
			return nil, nil, fmt.Errorf("GPU inference check: %w", err)
		}
	}
	return server, stop, nil
}

func (s *inferenceServer) waitReady(ctx context.Context) error {
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("wait for Whisper model: %w", readyCtx.Err())
		case <-s.done:
			return fmt.Errorf("whisper-server exited while loading: %v", s.err)
		case <-ticker.C:
			requestCtx, requestCancel := context.WithTimeout(readyCtx, time.Second)
			req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, s.url+"/health", nil)
			response, err := s.client.Do(req)
			ready := err == nil && response.StatusCode == http.StatusOK
			if response != nil {
				response.Body.Close()
			}
			requestCancel()
			if ready {
				return nil
			}
		}
	}
}

func (s *inferenceServer) translate(ctx context.Context, pcm []byte) (string, error) {
	audioContext := "768"
	if s.fullContext {
		audioContext = "0"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range [][2]string{
		{"language", "de"},
		{"translate", "true"},
		{"response_format", "json"},
		{"temperature", "0"},
		{"audio_ctx", audioContext},
	} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return "", err
		}
	}
	file, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(wav(pcm)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/inference", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("translate audio: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("Whisper returned %s: %s", response.Status, message)
	}
	var output struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&output); err != nil {
		return "", fmt.Errorf("decode translation: %w", err)
	}
	return strings.Join(strings.Fields(output.Text), " "), nil
}
