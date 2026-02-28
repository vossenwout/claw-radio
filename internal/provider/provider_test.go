package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYtDlpProviderResolveUsesCachedFileWithoutDownload(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cacheDir: %v", err)
	}

	cachedPath := filepath.Join(cacheDir, "abc123.opus")
	if err := os.WriteFile(cachedPath, []byte("cached"), 0o644); err != nil {
		t.Fatalf("write cached file: %v", err)
	}

	logPath := filepath.Join(tempDir, "yt-dlp.log")
	t.Setenv("YTDLP_LOG_PATH", logPath)
	t.Setenv("YTDLP_CANDIDATE_ID", "abc123")
	t.Setenv("YTDLP_CANDIDATE_URL", "https://www.youtube.com/watch?v=abc123")
	t.Setenv("YTDLP_EXPECT_QUERY", "ytsearch10:cached seed official audio")
	t.Setenv("YTDLP_FORBID_DOWNLOAD", "1")

	prov := NewYtDlpProvider(makeFakeYtDlpBinary(t))
	got, err := prov.Resolve("cached seed", cacheDir)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got != cachedPath {
		t.Fatalf("Resolve() path = %q, want %q", got, cachedPath)
	}

	calls := readCallLog(t, logPath)
	if len(calls) != 1 {
		t.Fatalf("expected 1 yt-dlp call, got %d (%v)", len(calls), calls)
	}
	if !strings.HasPrefix(calls[0], "--dump-json ") {
		t.Fatalf("first call = %q, want search call", calls[0])
	}
}

func TestYtDlpProviderResolveDownloadsWhenCacheMiss(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "cache")

	logPath := filepath.Join(tempDir, "yt-dlp.log")
	t.Setenv("YTDLP_LOG_PATH", logPath)
	t.Setenv("YTDLP_CANDIDATE_ID", "abc123")
	t.Setenv("YTDLP_CANDIDATE_URL", "https://www.youtube.com/watch?v=abc123")
	t.Setenv("YTDLP_EXPECT_QUERY", "ytsearch10:new seed official audio")

	prov := NewYtDlpProvider(makeFakeYtDlpBinary(t))
	got, err := prov.Resolve("new seed", cacheDir)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	want := filepath.Join(cacheDir, "abc123.opus")
	if got != want {
		t.Fatalf("Resolve() path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("downloaded file missing at %q: %v", want, err)
	}

	calls := readCallLog(t, logPath)
	if len(calls) != 2 {
		t.Fatalf("expected 2 yt-dlp calls, got %d (%v)", len(calls), calls)
	}
	if !strings.HasPrefix(calls[0], "--dump-json ") {
		t.Fatalf("first call = %q, want search call", calls[0])
	}
	if !strings.HasPrefix(calls[1], "-x --audio-format opus -o ") {
		t.Fatalf("second call = %q, want download call", calls[1])
	}
}

func TestYtDlpProviderResolveDirectURLSkipsSearch(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "cache")

	logPath := filepath.Join(tempDir, "yt-dlp.log")
	t.Setenv("YTDLP_LOG_PATH", logPath)
	t.Setenv("YTDLP_CANDIDATE_ID", "url123")
	t.Setenv("YTDLP_FORBID_SEARCH", "1")

	seedURL := "https://www.youtube.com/watch?v=url123"
	prov := NewYtDlpProvider(makeFakeYtDlpBinary(t))
	got, err := prov.Resolve(seedURL, cacheDir)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	want := filepath.Join(cacheDir, "url123.opus")
	if got != want {
		t.Fatalf("Resolve() path = %q, want %q", got, want)
	}

	calls := readCallLog(t, logPath)
	if len(calls) != 1 {
		t.Fatalf("expected 1 yt-dlp call, got %d (%v)", len(calls), calls)
	}
	if !strings.HasPrefix(calls[0], "-x --audio-format opus -o ") {
		t.Fatalf("call = %q, want download call", calls[0])
	}
	if strings.Contains(calls[0], "--dump-json") {
		t.Fatalf("call = %q, expected no search invocation", calls[0])
	}
}

func TestSpotifyProviderResolveNotImplemented(t *testing.T) {
	_, err := (&SpotifyProvider{}).Resolve("seed", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("Resolve() error = %v, want contains %q", err, "not implemented")
	}
}

func TestAppleMusicProviderResolveNotImplemented(t *testing.T) {
	_, err := (&AppleMusicProvider{}).Resolve("seed", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("Resolve() error = %v, want contains %q", err, "not implemented")
	}
}

func TestYtDlpProviderName(t *testing.T) {
	got := (&YtDlpProvider{}).Name()
	if got != "youtube" {
		t.Fatalf("Name() = %q, want %q", got, "youtube")
	}
}

func makeFakeYtDlpBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-yt-dlp")
	script := `#!/bin/sh
set -eu

if [ -n "${YTDLP_LOG_PATH:-}" ]; then
  printf '%s\n' "$*" >> "${YTDLP_LOG_PATH}"
fi

if [ "$#" -ge 1 ] && [ "$1" = "--dump-json" ]; then
  if [ "${YTDLP_FORBID_SEARCH:-}" = "1" ]; then
    echo "search not allowed" >&2
    exit 12
  fi
  if [ -n "${YTDLP_EXPECT_QUERY:-}" ] && [ "$2" != "${YTDLP_EXPECT_QUERY}" ]; then
    echo "unexpected query: $2" >&2
    exit 11
  fi

  id="${YTDLP_CANDIDATE_ID:-abc123}"
  url="${YTDLP_CANDIDATE_URL:-https://www.youtube.com/watch?v=${id}}"
  printf '{"id":"%s","title":"Test Song","duration":210,"webpage_url":"%s"}\n' "${id}" "${url}"
  exit 0
fi

if [ "$#" -ge 1 ] && [ "$1" = "-x" ]; then
  if [ "${YTDLP_FORBID_DOWNLOAD:-}" = "1" ]; then
    echo "download not allowed" >&2
    exit 13
  fi
  id="${YTDLP_CANDIDATE_ID:-abc123}"
  pattern="$5"
  out="$(printf '%s' "${pattern}" | sed "s/%(id)s/${id}/g; s/%(ext)s/opus/g")"
  mkdir -p "$(dirname "${out}")"
  : > "${out}"
  exit 0
fi

echo "unexpected args: $*" >&2
exit 14
`

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp binary: %v", err)
	}

	return path
}

func readCallLog(t *testing.T, logPath string) []string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	return filtered
}
