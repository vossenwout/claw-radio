package provider

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/vossenwout/claw-radio/internal/ytdlp"
)

type YtDlpProvider struct {
	Binary string
}

func NewYtDlpProvider(binary string) *YtDlpProvider {
	return &YtDlpProvider{Binary: binary}
}

func (p *YtDlpProvider) Resolve(seed, cacheDir string) (string, error) {
	trimmedSeed := strings.TrimSpace(seed)
	if trimmedSeed == "" {
		return "", fmt.Errorf("seed is empty")
	}

	if isHTTPURL(trimmedSeed) {
		return ytdlp.Download(p.Binary, trimmedSeed, cacheDir)
	}

	query := "ytsearch10:" + trimmedSeed + " official audio"
	candidate, err := ytdlp.BestCandidate(p.Binary, query)
	if err != nil {
		return "", fmt.Errorf("resolve seed %q: %w", trimmedSeed, err)
	}

	if candidate.ID != "" {
		cachedPath := filepath.Join(cacheDir, candidate.ID+".opus")
		if info, statErr := os.Stat(cachedPath); statErr == nil && !info.IsDir() {
			_ = writeTrackMetaSidecar(cachedPath, trimmedSeed, candidate)
			return cachedPath, nil
		}
	}

	downloadURL := strings.TrimSpace(candidate.WebpageURL)
	if downloadURL == "" {
		if candidate.ID == "" {
			return "", fmt.Errorf("no download URL for seed %q", trimmedSeed)
		}
		downloadURL = "https://www.youtube.com/watch?v=" + candidate.ID
	}

	path, err := ytdlp.Download(p.Binary, downloadURL, cacheDir)
	if err != nil {
		return "", fmt.Errorf("download seed %q: %w", trimmedSeed, err)
	}
	_ = writeTrackMetaSidecar(path, trimmedSeed, candidate)
	return path, nil
}

func (p *YtDlpProvider) Name() string {
	return "youtube"
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

type trackMetaSidecar struct {
	Seed    string `json:"seed"`
	Artist  string `json:"artist,omitempty"`
	Title   string `json:"title,omitempty"`
	Display string `json:"display,omitempty"`
	ID      string `json:"id,omitempty"`
}

func writeTrackMetaSidecar(audioPath, seed string, candidate *ytdlp.Candidate) error {
	trimmedPath := strings.TrimSpace(audioPath)
	if trimmedPath == "" {
		return nil
	}

	meta := trackMetaSidecar{Seed: strings.TrimSpace(seed)}
	artist, title := splitSeed(meta.Seed)
	meta.Artist = artist
	meta.Title = title
	if meta.Seed != "" {
		meta.Display = meta.Seed
	}
	if candidate != nil {
		meta.ID = strings.TrimSpace(candidate.ID)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	sidecarPath := trimmedPath + ".meta.json"
	return os.WriteFile(sidecarPath, data, 0o644)
}

func splitSeed(seed string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(seed), " - ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
