package subtitle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var vadResult = regexp.MustCompile(`(?m)^Detected ([0-9]+) speech segments:$`)

type speechDetector struct {
	modelPath string
	threshold float64
}

func (d speechDetector) hasSpeech(ctx context.Context, pcm []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	program := os.Getenv("AUDIO_SUBTITLE_VAD")
	if program == "" {
		program = "whisper-vad-speech-segments"
	}
	cmd := exec.CommandContext(ctx, program,
		"-np", "-t", "1", "-vm", d.modelPath, "-f", "-",
		"-vt", strconv.FormatFloat(d.threshold, 'f', -1, 64),
	)
	cmd.Stdin = bytes.NewReader(wav(pcm))
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("Silero VAD: %w: %s", err, diagnostics.String())
	}
	match := vadResult.FindSubmatch(output)
	if match == nil {
		return false, fmt.Errorf("unrecognized Silero VAD result: %s", output)
	}
	count, err := strconv.Atoi(string(match[1]))
	return count > 0, err
}
