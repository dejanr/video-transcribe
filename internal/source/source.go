package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dejanr/video-transcribe/internal/shell"
)

type Chapter struct {
	Title     string  `json:"title"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

type Metadata struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Channel      string    `json:"channel"`
	Uploader     string    `json:"uploader"`
	Duration     int       `json:"duration"`
	UploadDate   string    `json:"upload_date"`
	WebpageURL   string    `json:"webpage_url"`
	OriginalURL  string    `json:"original_url"`
	Description  string    `json:"description"`
	Extractor    string    `json:"extractor"`
	ExtractorKey string    `json:"extractor_key"`
	Chapters     []Chapter `json:"chapters"`
}

type Client struct {
	Runner shell.Runner
}

func (m Metadata) Platform() string {
	extractorKey := strings.ToLower(strings.TrimSpace(m.ExtractorKey))
	switch {
	case strings.HasPrefix(extractorKey, "twitch"):
		return "twitch"
	case strings.HasPrefix(extractorKey, "youtube"):
		return "youtube"
	}

	for _, rawURL := range []string{m.WebpageURL, m.OriginalURL} {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil {
			continue
		}
		host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
		switch host {
		case "youtube.com", "youtu.be", "m.youtube.com":
			return "youtube"
		case "twitch.tv", "m.twitch.tv", "clips.twitch.tv":
			return "twitch"
		}
	}
	return ""
}

func (c Client) FetchMetadata(ctx context.Context, url string) (Metadata, error) {
	output, err := c.Runner.Capture(ctx, "yt-dlp", "--no-warnings", "--dump-single-json", "--no-download", url)
	if err != nil {
		return Metadata{}, err
	}

	var metadata Metadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("decode yt-dlp metadata: %w", err)
	}
	if metadata.WebpageURL == "" {
		metadata.WebpageURL = url
	}
	if metadata.OriginalURL == "" {
		metadata.OriginalURL = url
	}
	return metadata, nil
}

func (c Client) DownloadAudio(ctx context.Context, url string, tempDir string, workspaceDir string, force bool) (string, error) {
	if !force {
		if existing, ok := existingAudio(workspaceDir); ok {
			return existing, nil
		}
	}

	outputTemplate := filepath.Join(tempDir, "audio.%(ext)s")
	args := []string{
		"--no-warnings",
		"--no-playlist",
		"-f", "bestaudio/best",
		"-o", outputTemplate,
		url,
	}
	if err := c.Runner.Run(ctx, "yt-dlp", args...); err != nil {
		return "", err
	}

	tempAudioPath, ok := existingAudio(tempDir)
	if !ok {
		return "", fmt.Errorf("audio download finished but no audio artifact was found in %s", tempDir)
	}
	finalAudioPath, err := publishAudio(tempAudioPath, workspaceDir)
	if err != nil {
		return "", err
	}
	return tempAudioPathOrFinal(tempAudioPath, finalAudioPath), nil
}

func existingAudio(workspaceDir string) (string, bool) {
	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		return "", false
	}

	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "audio.") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".wav" || ext == ".json" || ext == ".md" || ext == ".txt" {
			continue
		}
		matches = append(matches, filepath.Join(workspaceDir, name))
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[0], true
}

func publishAudio(tempAudioPath string, workspaceDir string) (string, error) {
	pattern := filepath.Join(workspaceDir, "audio.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("scan audio artifacts: %w", err)
	}
	for _, match := range matches {
		if strings.EqualFold(filepath.Ext(match), ".wav") {
			continue
		}
		_ = os.Remove(match)
	}

	finalAudioPath := filepath.Join(workspaceDir, filepath.Base(tempAudioPath))
	if err := shell.PublishFile(tempAudioPath, finalAudioPath); err != nil {
		return "", err
	}
	return finalAudioPath, nil
}

func tempAudioPathOrFinal(tempAudioPath string, finalAudioPath string) string {
	if strings.TrimSpace(tempAudioPath) != "" {
		return tempAudioPath
	}
	return finalAudioPath
}
