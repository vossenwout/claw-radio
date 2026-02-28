package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type configExampleFile struct {
	MPV struct {
		Binary string `json:"binary"`
	} `json:"mpv"`
	YtDlp struct {
		Binary string `json:"binary"`
	} `json:"ytdlp"`
	FFmpeg struct {
		Binary string `json:"binary"`
	} `json:"ffmpeg"`
	TTS struct {
		FallbackBinary string `json:"fallback_binary"`
	} `json:"tts"`
	Station struct {
		QueueDepth int `json:"queue_depth"`
	} `json:"station"`
	Search struct {
		SearxNGURL string `json:"searxng_url"`
	} `json:"search"`
}

func TestConfigExampleLoadsViaEnvPath(t *testing.T) {
	repoRoot := testRepoRoot(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Chdir(%q) failed: %v", repoRoot, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	t.Setenv("CLAW_RADIO_CONFIG", "config.example.json")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	if _, err := Load(); err != nil {
		t.Fatalf("Load() with config.example.json returned error: %v", err)
	}
}

func TestConfigExampleContainsExpectedKeysAndDefaults(t *testing.T) {
	repoRoot := testRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "config.example.json"))
	if err != nil {
		t.Fatalf("ReadFile(config.example.json) error: %v", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("json.Unmarshal(top-level) error: %v", err)
	}

	for _, key := range []string{"mpv", "ytdlp", "ffmpeg", "tts", "station", "search"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("expected top-level key %q to be present", key)
		}
	}

	var cfg configExampleFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("json.Unmarshal(config.example.json) error: %v", err)
	}

	if cfg.MPV.Binary != "" {
		t.Fatalf("expected mpv.binary to be empty string, got %q", cfg.MPV.Binary)
	}
	if cfg.YtDlp.Binary != "" {
		t.Fatalf("expected ytdlp.binary to be empty string, got %q", cfg.YtDlp.Binary)
	}
	if cfg.FFmpeg.Binary != "" {
		t.Fatalf("expected ffmpeg.binary to be empty string, got %q", cfg.FFmpeg.Binary)
	}
	if cfg.TTS.FallbackBinary != "" {
		t.Fatalf("expected tts.fallback_binary to be empty string, got %q", cfg.TTS.FallbackBinary)
	}
	if cfg.Station.QueueDepth != 5 {
		t.Fatalf("expected station.queue_depth to be 5, got %d", cfg.Station.QueueDepth)
	}
	if cfg.Search.SearxNGURL != defaultSearchSearxURL {
		t.Fatalf("expected search.searxng_url to be %q, got %q", defaultSearchSearxURL, cfg.Search.SearxNGURL)
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller(0) failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}
