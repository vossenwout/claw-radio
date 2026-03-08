package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

func TestSayWhenMPVNotRunningQueuesIntroForNextStart(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		Station: config.StationConfig{
			StateDir: stateDir,
		},
		TTS: config.TTSConfig{
			DataDir: t.TempDir(),
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	if !strings.Contains(stdout, "queued intro banter for next start") {
		t.Fatalf("stdout = %q, want intro queue message", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "pending-intro.json")); statErr != nil {
		t.Fatalf("pending intro file missing: %v", statErr)
	}
	metaPath := filepath.Join(cfg.TTS.DataDir, "banter", "123456789.wav.meta.json")
	data, readErr := os.ReadFile(metaPath)
	if readErr != nil {
		t.Fatalf("banter metadata missing: %v", readErr)
	}
	if !strings.Contains(string(data), `"text":"hello"`) {
		t.Fatalf("banter metadata = %q, want text payload", string(data))
	}
}

func TestSayWhenTTSUnavailableExitsFour(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-unavailable")
	_ = startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		TTS: config.TTSConfig{
			DataDir: t.TempDir(),
		},
	}
	ttsClient := &fakeSayTTSClient{
		err: errors.New("No TTS binary found. Install: claw-radio tts install  OR  apt install espeak-ng"),
	}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "hello")
	assertExitCode(t, err, 4)
}

func TestSayOnSuccessPrintsQueuedBanterAndExitsZero(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-success")
	server := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		TTS: config.TTSConfig{
			DataDir: t.TempDir(),
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	if !strings.Contains(stdout, "queued banter") {
		t.Fatalf("stdout = %q, want contains %q", stdout, "queued banter")
	}
	if ttsClient.lastOutPath == "" {
		t.Fatal("expected render out path to be set")
	}
	if filepath.Dir(ttsClient.lastOutPath) != filepath.Join(cfg.TTS.DataDir, "banter") {
		t.Fatalf("out path dir = %q, want %q", filepath.Dir(ttsClient.lastOutPath), filepath.Join(cfg.TTS.DataDir, "banter"))
	}
	if ttsClient.lastVoicePath != "" {
		t.Fatalf("voice path = %q, want empty string", ttsClient.lastVoicePath)
	}
	data, readErr := os.ReadFile(ttsClient.lastOutPath + ".meta.json")
	if readErr != nil {
		t.Fatalf("banter metadata missing: %v", readErr)
	}
	if !strings.Contains(string(data), `"text":"hello"`) {
		t.Fatalf("banter metadata = %q, want text payload", string(data))
	}
}

func TestSayWithoutTextArgumentExitsTwo(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		TTS: config.TTSConfig{
			DataDir: t.TempDir(),
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("say")
	assertExitCode(t, err, 2)
	if !strings.Contains(stdout+stderr, "Usage:") {
		t.Fatalf("usage output missing: stdout=%q stderr=%q", stdout, stderr)
	}
}

type fakeSayTTSClient struct {
	lastText      string
	lastVoicePath string
	lastOutPath   string
	err           error
}

func (f *fakeSayTTSClient) Render(text, voicePath, outPath string) error {
	f.lastText = text
	f.lastVoicePath = voicePath
	f.lastOutPath = outPath
	return f.err
}

func withSayTestHooks(cfg *config.Config, ttsClient sayTTSRenderer) func() {
	origLoad := loadConfigFn
	origDial := dialPlaybackClientFn
	origTTS := newSayTTSClientFn
	origNow := nowUnixNanoFn

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	dialPlaybackClientFn = func(socketPath string) (playbackClient, error) {
		return mpv.Dial(socketPath)
	}
	newSayTTSClientFn = func(*config.Config) sayTTSRenderer {
		return ttsClient
	}
	nowUnixNanoFn = func() int64 {
		return 123456789
	}

	return func() {
		loadConfigFn = origLoad
		dialPlaybackClientFn = origDial
		newSayTTSClientFn = origTTS
		nowUnixNanoFn = origNow
	}
}
