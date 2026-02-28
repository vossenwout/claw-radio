package ytdlp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Candidate struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Uploader   string  `json:"uploader"`
	Duration   float64 `json:"duration"`
	ViewCount  int64   `json:"view_count"`
	IsLive     bool    `json:"is_live"`
	LiveStatus string  `json:"live_status"`
	WebpageURL string  `json:"webpage_url"`
}

var execCommand = exec.Command

var (
	noiseWordRe = regexp.MustCompile(`\b(official|audio|video|ft|feat|hd|4k)\b`)
	nonWordRe   = regexp.MustCompile(`[^a-z0-9\-]+`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

func Search(binary, query string, n int) ([]Candidate, error) {
	bin := binaryOrDefault(binary)
	searchQuery := buildSearchQuery(query, n)

	cmd := execCommand(bin, "--dump-json", searchQuery)
	out, err := cmd.Output()
	if err != nil {
		return nil, wrapExecError(err, "search failed")
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	candidates := make([]Candidate, 0)
	for {
		var c Candidate
		if err := dec.Decode(&c); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse yt-dlp output: %w", err)
		}
		candidates = append(candidates, c)
	}

	return candidates, nil
}

func Score(c Candidate) int {
	score := 0

	title := strings.ToLower(c.Title)
	uploader := strings.ToLower(c.Uploader)

	if strings.Contains(title, "provided to youtube") || strings.HasSuffix(uploader, " - topic") {
		score += 70
	}
	if strings.Contains(uploader, "vevo") {
		score += 45
	}
	if strings.Contains(title, "official audio") {
		score += 40
	}
	if strings.Contains(title, "official") && !strings.Contains(title, "video") {
		score += 10
	}
	if strings.Contains(title, "audio") {
		score += 8
	}

	penalties := []string{
		"live",
		"cover",
		"karaoke",
		"lyrics",
		"lyric",
		"remix",
		"mix",
		"sped up",
		"slowed",
		"reverb",
		"8d",
		"nightcore",
		"instrumental",
		"extended",
		"1 hour",
		"full album",
		"playlist",
	}
	for _, p := range penalties {
		if strings.Contains(title, p) {
			score -= 30
		}
	}

	switch {
	case c.Duration > 0 && c.Duration < 90:
		score -= 80
	case c.Duration > 8*60:
		score -= 50
	case c.Duration >= 120 && c.Duration <= 420:
		score += 12
	}

	if c.IsLive {
		score -= 120
	}

	liveStatus := strings.ToLower(strings.TrimSpace(c.LiveStatus))
	if liveStatus != "" && liveStatus != "not_live" {
		score -= 60
	}

	switch {
	case c.ViewCount >= 50_000_000:
		score += 18
	case c.ViewCount >= 5_000_000:
		score += 14
	case c.ViewCount >= 500_000:
		score += 10
	case c.ViewCount >= 50_000:
		score += 6
	}

	titleLen := len(c.Title)
	if titleLen > 60 {
		score -= (titleLen - 60) / 10
	}

	return score
}

func BestCandidate(binary, query string) (*Candidate, error) {
	candidates, err := Search(binary, query, 10)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates found for query %q", query)
	}

	best := candidates[0]
	bestScore := Score(best)
	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]
		score := Score(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}

	return &best, nil
}

func ResolveURL(binary, rawURL string) (string, error) {
	bin := binaryOrDefault(binary)
	cmd := execCommand(bin, "--get-url", "--format", "bestaudio", rawURL)
	out, err := cmd.Output()
	if err != nil {
		return "", wrapExecError(err, "resolve url failed")
	}

	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("resolve url returned empty output for %q", rawURL)
}

func Download(binary, rawURL, outDir string) (string, error) {
	bin := binaryOrDefault(binary)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir %s: %w", outDir, err)
	}

	pattern := filepath.Join(outDir, "%(id)s.%(ext)s")
	cmd := execCommand(bin, "-x", "--audio-format", "opus", "-o", pattern, rawURL)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return "", wrapExecErrorWithOutput(err, combined, "download failed")
	}

	files, err := os.ReadDir(outDir)
	if err != nil {
		return "", fmt.Errorf("read output dir %s: %w", outDir, err)
	}

	var newestPath string
	var newestTime int64
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		info, statErr := file.Info()
		if statErr != nil {
			continue
		}
		mod := info.ModTime().UnixNano()
		if newestPath == "" || mod > newestTime {
			newestTime = mod
			newestPath = filepath.Join(outDir, file.Name())
		}
	}

	if newestPath == "" {
		return "", fmt.Errorf("download succeeded but no file found in %s", outDir)
	}
	return newestPath, nil
}

func NormalizeSongKey(human string) string {
	s := strings.ToLower(human)
	s = nonWordRe.ReplaceAllString(s, " ")
	s = noiseWordRe.ReplaceAllString(s, " ")
	s = spaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " - ", " - ")
	return s
}

func buildSearchQuery(query string, n int) string {
	if n <= 0 {
		n = 1
	}
	trimmed := strings.TrimSpace(query)
	if strings.HasPrefix(strings.ToLower(trimmed), "ytsearch") {
		return trimmed
	}
	return fmt.Sprintf("ytsearch%d:%s", n, trimmed)
}

func binaryOrDefault(binary string) string {
	bin := strings.TrimSpace(binary)
	if bin == "" {
		return "yt-dlp"
	}
	return bin
}

func wrapExecError(err error, prefix string) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%s: %s", prefix, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func wrapExecErrorWithOutput(err error, out []byte, prefix string) error {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %s", prefix, text)
}
