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
		SearxNGURL            string   `json:"searxng_url"`
		MaxSearchHits         int      `json:"max_search_hits"`
		MaxPages              int      `json:"max_pages"`
		FetchConcurrency      int      `json:"fetch_concurrency"`
		RequestTimeoutSeconds int      `json:"request_timeout_seconds"`
		UserAgent             string   `json:"user_agent"`
		EnableQueryExpansion  bool     `json:"enable_query_expansion"`
		Debug                 bool     `json:"debug"`
		Engines               []string `json:"engines"`
		ModeEngines           struct {
			Raw        []string `json:"raw"`
			ArtistTop  []string `json:"artist_top"`
			ArtistYear []string `json:"artist_year"`
			ChartYear  []string `json:"chart_year"`
			GenreTop   []string `json:"genre_top"`
		} `json:"mode_engines"`
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
	if cfg.Search.MaxSearchHits != 20 {
		t.Fatalf("expected search.max_search_hits to be 20, got %d", cfg.Search.MaxSearchHits)
	}
	if cfg.Search.MaxPages != 20 {
		t.Fatalf("expected search.max_pages to be 20, got %d", cfg.Search.MaxPages)
	}
	if cfg.Search.FetchConcurrency != 6 {
		t.Fatalf("expected search.fetch_concurrency to be 6, got %d", cfg.Search.FetchConcurrency)
	}
	if cfg.Search.RequestTimeoutSeconds != 30 {
		t.Fatalf("expected search.request_timeout_seconds to be 30, got %d", cfg.Search.RequestTimeoutSeconds)
	}
	if cfg.Search.UserAgent != defaultSearchUserAgent {
		t.Fatalf("expected search.user_agent to be %q, got %q", defaultSearchUserAgent, cfg.Search.UserAgent)
	}
	if cfg.Search.EnableQueryExpansion {
		t.Fatalf("expected search.enable_query_expansion to be false")
	}
	if cfg.Search.Debug {
		t.Fatalf("expected search.debug to be false")
	}
	if len(cfg.Search.Engines) != 0 {
		t.Fatalf("expected search.engines to be empty array by default, got %v", cfg.Search.Engines)
	}
	if len(cfg.Search.ModeEngines.Raw) != 0 || len(cfg.Search.ModeEngines.ArtistTop) != 0 || len(cfg.Search.ModeEngines.ArtistYear) != 0 || len(cfg.Search.ModeEngines.ChartYear) != 0 || len(cfg.Search.ModeEngines.GenreTop) != 0 {
		t.Fatalf("expected search.mode_engines lists to be empty arrays by default, got %+v", cfg.Search.ModeEngines)
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
