package whisper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const sileroModelName = "ggml-silero-v6.2.0.bin"
const sileroModelURL = "https://huggingface.co/ggml-org/whisper-vad/resolve/9ffd54a1e1ee413ddf265af9913beaf518d1639b/" + sileroModelName
const sileroModelSHA256 = "2aa269b785eeb53a82983a20501ddf7c1d9c48e33ab63a41391ac6c9f7fb6987"

func (c Client) EnsureVADModel(ctx context.Context, modelDir string) (string, error) {
	return ensureVADModel(ctx, modelDir, sileroModelURL, sileroModelSHA256)
}

func ensureVADModel(ctx context.Context, modelDir, url, checksum string) (string, error) {
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(modelDir, sileroModelName)
	if body, err := os.ReadFile(path); err == nil {
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) == checksum {
			return path, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download Silero VAD: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download Silero VAD: %s", response.Status)
	}
	file, err := os.CreateTemp(modelDir, ".silero-download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, 10<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != checksum {
		return "", fmt.Errorf("Silero VAD checksum mismatch; download was not published")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
