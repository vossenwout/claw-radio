package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

	fetchYouTubeOEmbedFn = func(videoID string) (string, string, error) {
		client := &http.Client{Timeout: 1200 * time.Millisecond}
		u := "https://www.youtube.com/oembed?url=" +
			url.QueryEscape("https://www.youtube.com/watch?v="+videoID) +
			"&format=json"

		resp, err := client.Get(u)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("oembed status %d", resp.StatusCode)
		}

		var payload struct {
			Title      string `json:"title"`
			AuthorName string `json:"author_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", "", err
		}
		return strings.TrimSpace(payload.AuthorName), strings.TrimSpace(payload.Title), nil
	}
)

type playbackPropertyReader interface {
	Get(prop string) (json.RawMessage, error)
}

func resolvePlaybackTitle(reader playbackPropertyReader) string {
	mediaTitle, _ := readStringPropertyCompat(reader, "media-title")
	mediaTitle = strings.TrimSpace(mediaTitle)

	if mediaTitle != "" && !looksLikeFilenameTitle(mediaTitle) {
		return mediaTitle
	}

	if metadataTitle := resolveTitleFromMetadata(reader); metadataTitle != "" {
		return metadataTitle
	}

	if sidecarTitle := resolveTitleFromSidecar(reader); sidecarTitle != "" {
		return sidecarTitle
	}

	if mediaTitle != "" {
		if yt := resolveTitleFromYouTubeID(trimMediaFilename(mediaTitle)); yt != "" {
			return yt
		}
	}

	if mediaTitle != "" {
		return trimMediaFilename(mediaTitle)
	}

	if filename, ok := readStringPropertyCompat(reader, "filename"); ok && strings.TrimSpace(filename) != "" {
		return trimMediaFilename(filename)
	}

	return ""
}

func resolveTitleFromMetadata(reader playbackPropertyReader) string {
	raw, err := reader.Get("metadata")
	if err != nil {
		return ""
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return ""
	}

	artist := metadataValue(metadata, "artist")
	title := metadataValue(metadata, "title")
	if title == "" {
		title = metadataValue(metadata, "track")
	}

	if title != "" && artist != "" {
		return artist + " - " + title
	}
	if title != "" {
		return title
	}

	return ""
}

func metadataValue(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	for k, raw := range metadata {
		if strings.ToLower(strings.TrimSpace(k)) != lowerKey {
			continue
		}
		if v, ok := raw.(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readStringPropertyCompat(reader playbackPropertyReader, prop string) (string, bool) {
	raw, err := reader.Get(prop)
	if err != nil {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func looksLikeFilenameTitle(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" {
		return false
	}
	for _, ext := range []string{".opus", ".m4a", ".mp3", ".webm", ".ogg", ".wav", ".flac", ".aac", ".mp4", ".mkv", ".mov"} {
		if strings.HasSuffix(v, ext) {
			return true
		}
	}
	return false
}

func trimMediaFilename(value string) string {
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" {
		return ""
	}
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

type trackMetaSidecar struct {
	Seed    string `json:"seed"`
	Artist  string `json:"artist"`
	Title   string `json:"title"`
	Display string `json:"display"`
}

func resolveTitleFromSidecar(reader playbackPropertyReader) string {
	pathValue, ok := readStringPropertyCompat(reader, "path")
	if !ok || strings.TrimSpace(pathValue) == "" {
		if filename, ok := readStringPropertyCompat(reader, "filename"); ok {
			pathValue = filename
		}
	}
	if strings.TrimSpace(pathValue) == "" {
		return ""
	}

	sidecarPath := strings.TrimSpace(pathValue) + ".meta.json"
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return ""
	}

	var meta trackMetaSidecar
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	if strings.TrimSpace(meta.Display) != "" {
		return strings.TrimSpace(meta.Display)
	}
	if strings.TrimSpace(meta.Artist) != "" && strings.TrimSpace(meta.Title) != "" {
		return strings.TrimSpace(meta.Artist) + " - " + strings.TrimSpace(meta.Title)
	}
	if strings.TrimSpace(meta.Seed) != "" {
		return strings.TrimSpace(meta.Seed)
	}
	return ""
}

func resolveTitleFromYouTubeID(value string) string {
	trimmed := strings.TrimSpace(value)
	if !youtubeIDRe.MatchString(trimmed) {
		return ""
	}

	artist, title, err := fetchYouTubeOEmbedFn(trimmed)
	if err != nil {
		return ""
	}
	if title == "" {
		return ""
	}
	if artist != "" {
		return artist + " - " + title
	}
	return title
}
