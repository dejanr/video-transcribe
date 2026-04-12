package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dejanr/video-transcribe/internal/shell"
)

type Client struct {
	Runner shell.Runner
}

type Output struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []Segment `json:"transcription"`
}

type Segment struct {
	Timestamps struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"timestamps"`
	Offsets struct {
		From int `json:"from"`
		To   int `json:"to"`
	} `json:"offsets"`
	Text string `json:"text"`
}

type ffmpegOutputSegment struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

func (c Client) EnsureModel(ctx context.Context, modelDir string, modelName string) (string, error) {
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	modelPath := filepath.Join(modelDir, fmt.Sprintf("ggml-%s.bin", modelName))
	if _, err := os.Stat(modelPath); err == nil {
		return modelPath, nil
	}

	if err := c.Runner.RunInDir(ctx, modelDir, "whisper-cpp-download-ggml-model", modelName); err != nil {
		return "", err
	}
	if _, err := os.Stat(modelPath); err != nil {
		return "", fmt.Errorf("downloaded model is missing: %w", err)
	}
	return modelPath, nil
}

func (c Client) Transcribe(ctx context.Context, inputAudioPath string, tempDir string, workspaceDir string, modelPath string, language string, useGPU bool, force bool) (string, Output, error) {
	finalPath := filepath.Join(workspaceDir, "transcript.raw.json")
	if !force {
		if output, err := readOutput(finalPath); err == nil {
			return finalPath, output, nil
		}
	}

	tempBasePath := filepath.Join(tempDir, "transcript.raw")
	tempJSONPath := tempBasePath + ".json"
	args := whisperCLIArgs(modelPath, inputAudioPath, tempBasePath, language, useGPU)
	if err := c.Runner.Run(ctx, "whisper-cli", args...); err != nil {
		return "", Output{}, err
	}

	output, err := readOutput(tempJSONPath)
	if err != nil {
		return "", Output{}, err
	}
	if err := shell.PublishFile(tempJSONPath, finalPath); err != nil {
		return "", Output{}, err
	}
	return finalPath, output, nil
}

func (c Client) TranscribeWithFFmpeg(ctx context.Context, inputAudioPath string, tempDir string, workspaceDir string, modelPath string, language string, useGPU bool, gpuDevice int, queueSeconds int, totalDurationSeconds int, force bool) (string, Output, error) {
	finalPath := filepath.Join(workspaceDir, "transcript.raw.json")
	if !force {
		if output, err := readOutput(finalPath); err == nil {
			return finalPath, output, nil
		}
	}

	if queueSeconds <= 0 {
		queueSeconds = 15
	}

	tempFFmpegJSONPath := filepath.Join(tempDir, "transcript.raw.ffmpeg.json")
	tempJSONPath := filepath.Join(tempDir, "transcript.raw.json")
	stopProgress := startFFmpegProgressReporter(tempFFmpegJSONPath, totalDurationSeconds)
	defer stopProgress()
	filter := ffmpegWhisperFilter(modelPath, language, tempFFmpegJSONPath, useGPU, gpuDevice, queueSeconds)
	args := []string{
		"-nostdin",
		"-v", "warning",
		"-y",
		"-i", inputAudioPath,
		"-vn",
		"-af", filter,
		"-f", "null",
		"-",
	}
	if err := c.Runner.Run(ctx, "ffmpeg", args...); err != nil {
		return "", Output{}, err
	}

	output, err := readFFmpegOutput(tempFFmpegJSONPath, language)
	if err != nil {
		return "", Output{}, err
	}
	if err := shell.WriteJSON(tempJSONPath, output); err != nil {
		return "", Output{}, err
	}
	if err := shell.PublishFile(tempJSONPath, finalPath); err != nil {
		return "", Output{}, err
	}
	return finalPath, output, nil
}

func whisperCLIArgs(modelPath string, inputAudioPath string, outputBasePath string, language string, useGPU bool) []string {
	if language == "" {
		language = "auto"
	}
	args := []string{
		"-m", modelPath,
		"-f", inputAudioPath,
		"-ojf",
		"-of", outputBasePath,
		"-l", language,
		"-np",
	}
	if !useGPU {
		args = append(args, "-ng")
	}
	return args
}

func ffmpegWhisperFilter(modelPath string, language string, destination string, useGPU bool, gpuDevice int, queueSeconds int) string {
	if language == "" {
		language = "auto"
	}
	if gpuDevice < 0 {
		gpuDevice = 0
	}
	parts := []string{
		"whisper=model=" + escapeFFmpegFilterValue(modelPath),
		"language=" + escapeFFmpegFilterValue(language),
		fmt.Sprintf("queue=%d", queueSeconds),
		fmt.Sprintf("use_gpu=%t", useGPU),
		"destination=" + escapeFFmpegFilterValue(destination),
		"format=json",
	}
	if useGPU {
		parts = append(parts, fmt.Sprintf("gpu_device=%d", gpuDevice))
	}
	return strings.Join(parts, ":")
}

func readOutput(path string) (Output, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Output{}, fmt.Errorf("read whisper output: %w", err)
	}
	var output Output
	if err := json.Unmarshal(body, &output); err != nil {
		return Output{}, fmt.Errorf("decode whisper output: %w", err)
	}
	for i := range output.Transcription {
		output.Transcription[i].Text = normalizeText(output.Transcription[i].Text)
	}
	return output, nil
}

func readFFmpegOutput(path string, language string) (Output, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Output{}, fmt.Errorf("read ffmpeg whisper output: %w", err)
	}
	segments, err := decodeFFmpegSegments(body)
	if err != nil {
		return Output{}, fmt.Errorf("decode ffmpeg whisper output: %w", err)
	}

	output := Output{}
	output.Result.Language = language
	output.Transcription = make([]Segment, 0, len(segments))
	for _, value := range segments {
		text := normalizeText(value.Text)
		if text == "" {
			continue
		}
		segment := Segment{Text: text}
		segment.Offsets.From = value.Start
		segment.Offsets.To = value.End
		segment.Timestamps.From = formatMilliseconds(value.Start)
		segment.Timestamps.To = formatMilliseconds(value.End)
		output.Transcription = append(output.Transcription, segment)
	}
	return output, nil
}

func decodeFFmpegSegments(body []byte) ([]ffmpegOutputSegment, error) {
	return decodeFFmpegSegmentsWithPartial(body, false)
}

func decodeFFmpegSegmentsWithPartial(body []byte, allowPartial bool) ([]ffmpegOutputSegment, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var segments []ffmpegOutputSegment
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &segments); err != nil {
			if allowPartial {
				return segments, nil
			}
			return nil, err
		}
		return segments, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	for {
		var segment ffmpegOutputSegment
		if err := decoder.Decode(&segment); err != nil {
			if err == io.EOF {
				break
			}
			if allowPartial {
				return segments, nil
			}
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func startFFmpegProgressReporter(path string, totalDurationSeconds int) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastEndMS := -1
		for {
			select {
			case <-ticker.C:
				segments, err := readFFmpegProgress(path)
				if err != nil || len(segments) == 0 {
					continue
				}
				endMS := segments[len(segments)-1].End
				if endMS <= lastEndMS {
					continue
				}
				lastEndMS = endMS
				fmt.Fprintf(os.Stderr, "[progress %s] ffmpeg-whisper %s", time.Now().Format(time.RFC3339), formatProgress(endMS, totalDurationSeconds, len(segments)))
			case <-done:
				return
			}
		}
	}()
	return func() {
		select {
		case <-done:
			return
		default:
			close(done)
			segments, err := readFFmpegProgress(path)
			if err == nil && len(segments) > 0 {
				endMS := segments[len(segments)-1].End
				fmt.Fprintf(os.Stderr, "[progress %s] ffmpeg-whisper %s", time.Now().Format(time.RFC3339), formatProgress(endMS, totalDurationSeconds, len(segments)))
			}
		}
	}
}

func readFFmpegProgress(path string) ([]ffmpegOutputSegment, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeFFmpegSegmentsWithPartial(body, true)
}

func formatProgress(endMS int, totalDurationSeconds int, segmentCount int) string {
	current := formatMilliseconds(endMS)
	if totalDurationSeconds <= 0 {
		return fmt.Sprintf("current=%s segments=%d\n", current, segmentCount)
	}
	totalMS := totalDurationSeconds * 1000
	percent := (float64(endMS) / float64(totalMS)) * 100
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("current=%s total=%s %.1f%% segments=%d\n", current, formatMilliseconds(totalMS), percent, segmentCount)
}

func escapeFFmpegFilterValue(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`:`, `\:`,
		`'`, `\'`,
		`,`, `\,`,
	)
	return replacer.Replace(value)
}

func formatMilliseconds(value int) string {
	if value <= 0 {
		return "00:00:00.000"
	}
	duration := time.Duration(value) * time.Millisecond
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	seconds := int(duration / time.Second)
	duration -= time.Duration(seconds) * time.Second
	milliseconds := int(duration / time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds)
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
