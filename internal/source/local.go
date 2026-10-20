package source

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dejanr/video-transcribe/internal/shell"
)

func localPath(input string) (string, error) {
	path := input
	if strings.HasPrefix(strings.ToLower(input), "file:") {
		parsed, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("parse file URL: %w", err)
		}
		if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
			return "", fmt.Errorf("file URL must refer to a local file")
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || !filepath.IsAbs(parsed.Path) {
			return "", fmt.Errorf("file URL must contain an absolute path without credentials, query, or fragment")
		}
		path = parsed.Path
	} else if strings.Contains(input, "://") {
		return "", nil
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve local input: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat local input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("local input must be a regular file: %s", path)
	}
	return path, nil
}

func (c Client) fetchLocalMetadata(ctx context.Context, path string) (Metadata, error) {
	output, err := c.Runner.Capture(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", path)
	if err != nil {
		return Metadata{}, err
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return Metadata{}, fmt.Errorf("decode ffprobe metadata: %w", err)
	}
	hasAudio := false
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			hasAudio = true
			break
		}
	}
	if !hasAudio {
		return Metadata{}, fmt.Errorf("local input has no audio stream: %s", path)
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0 {
		return Metadata{}, fmt.Errorf("invalid local audio duration %q", probe.Format.Duration)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("stat local input: %w", err)
	}
	identity := fmt.Sprintf("%s\n%d\n%d", path, info.Size(), info.ModTime().UnixNano())
	hash := sha256.Sum256([]byte(identity))
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	return Metadata{
		ID:           fmt.Sprintf("local-%x", hash[:6]),
		Title:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Duration:     int(math.Ceil(duration)),
		WebpageURL:   fileURL,
		OriginalURL:  fileURL,
		Extractor:    "local",
		ExtractorKey: "Local",
	}, nil
}

func copyLocalAudio(path string, tempDir string, workspaceDir string) (string, error) {
	name := "source" + filepath.Ext(path)
	tempPath := filepath.Join(tempDir, name)
	if err := shell.PublishFile(path, tempPath); err != nil {
		return "", err
	}
	if err := shell.PublishFile(tempPath, filepath.Join(workspaceDir, name)); err != nil {
		return "", err
	}
	return tempPath, nil
}
