package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadNoConfigFileReturnsDefaults(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("CLAW_RADIO_CONFIG", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.MPV.Socket != defaultMPVSocket {
		t.Fatalf("unexpected mpv socket: %q", cfg.MPV.Socket)
	}
	if cfg.MPV.Log != filepath.Join(home, ".local", "share", "claw-radio", "mpv.log") {
		t.Fatalf("unexpected mpv log path: %q", cfg.MPV.Log)
	}
	if cfg.TTS.Socket != defaultTTSSocket {
		t.Fatalf("unexpected tts socket: %q", cfg.TTS.Socket)
	}
	if cfg.TTS.DataDir != filepath.Join(home, ".local", "share", "claw-radio") {
		t.Fatalf("unexpected tts data dir: %q", cfg.TTS.DataDir)
	}
	if cfg.Station.QueueDepth != 5 {
		t.Fatalf("unexpected queue depth: %d", cfg.Station.QueueDepth)
	}
	if cfg.Station.CacheDir != filepath.Join(home, ".local", "share", "claw-radio", "cache") {
		t.Fatalf("unexpected station cache dir: %q", cfg.Station.CacheDir)
	}
	if cfg.Station.StateDir != filepath.Join(home, ".local", "share", "claw-radio", "state") {
		t.Fatalf("unexpected station state dir: %q", cfg.Station.StateDir)
	}
	if cfg.Search.SearxNGURL != defaultSearchSearxURL {
		t.Fatalf("unexpected searxng url: %q", cfg.Search.SearxNGURL)
	}
	if cfg.MPV.Binary != "" {
		t.Fatalf("expected empty mpv binary, got %q", cfg.MPV.Binary)
	}
}

func TestLoadOverridesFromEnvironmentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	content := `{
		"mpv": {"socket": "/tmp/custom-mpv.sock"},
		"station": {"queue_depth": 9},
		"search": {"searxng_url": "http://localhost:9999"},
		"tts": {"voices": {"pop": "~/voices/pop.wav"}}
	}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	t.Setenv("CLAW_RADIO_CONFIG", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.MPV.Socket != "/tmp/custom-mpv.sock" {
		t.Fatalf("expected mpv socket override, got %q", cfg.MPV.Socket)
	}
	if cfg.Station.QueueDepth != 9 {
		t.Fatalf("expected queue depth override, got %d", cfg.Station.QueueDepth)
	}
	if cfg.Search.SearxNGURL != "http://localhost:9999" {
		t.Fatalf("expected search override, got %q", cfg.Search.SearxNGURL)
	}
	if cfg.TTS.Voices["pop"] != filepath.Join(home, "voices", "pop.wav") {
		t.Fatalf("expected expanded voice path override, got %q", cfg.TTS.Voices["pop"])
	}
	if _, ok := cfg.TTS.Voices["country"]; !ok {
		t.Fatalf("expected default voices map keys to remain populated")
	}
}

func TestLoadMalformedConfigIncludesPathInError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "bad-config.json")
	if err := os.WriteFile(configPath, []byte(`{"mpv":`), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	t.Setenv("CLAW_RADIO_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatalf("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Fatalf("expected error to contain config path %q, got %v", configPath, err)
	}
}

func TestLoadResolvesMPVBinaryFromLookPath(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("CLAW_RADIO_CONFIG", "")

	mpvPath := makeExecutable(t, binDir, "mpv")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.MPV.Binary != mpvPath {
		t.Fatalf("expected mpv binary %q, got %q", mpvPath, cfg.MPV.Binary)
	}
}

func TestLoadExpandsTildeInTTSDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	content := `{"tts": {"data_dir": "~/custom-data"}}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	t.Setenv("CLAW_RADIO_CONFIG", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	want := filepath.Join(home, "custom-data")
	if cfg.TTS.DataDir != want {
		t.Fatalf("expected expanded tts data dir %q, got %q", want, cfg.TTS.DataDir)
	}
}

func TestLoadFallbackBinaryResolution(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("CLAW_RADIO_CONFIG", "")

	var want string
	if runtime.GOOS == "darwin" {
		want = makeExecutable(t, binDir, "say")
	} else {
		want = makeExecutable(t, binDir, "espeak-ng")
		makeExecutable(t, binDir, "espeak")
		makeExecutable(t, binDir, "festival")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.TTS.FallbackBinary != want {
		t.Fatalf("expected fallback binary %q, got %q", want, cfg.TTS.FallbackBinary)
	}
}

func makeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", path, err)
	}
	return path
}
