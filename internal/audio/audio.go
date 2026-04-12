package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dejanr/video-transcribe/internal/shell"
)

type Normalizer struct {
	Runner shell.Runner
}

func (n Normalizer) Normalize(ctx context.Context, inputPath string, tempDir string, workspaceDir string, force bool) (string, error) {
	finalPath := filepath.Join(workspaceDir, "audio.wav")
	if !force {
		if _, err := os.Stat(finalPath); err == nil {
			return finalPath, nil
		}
	}

	tempPath := filepath.Join(tempDir, "audio.wav")
	if err := n.Runner.Run(ctx, "ffmpeg",
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		tempPath,
	); err != nil {
		return "", err
	}

	if _, err := os.Stat(tempPath); err != nil {
		return "", fmt.Errorf("normalized audio was not created: %w", err)
	}
	if err := shell.PublishFile(tempPath, finalPath); err != nil {
		return "", err
	}
	return tempPath, nil
}
