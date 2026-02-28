package provider

import (
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
