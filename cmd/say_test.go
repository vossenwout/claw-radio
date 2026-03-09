package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
	"github.com/vossenwout/claw-radio/internal/station"
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

func TestSayWhileIdleUsesAppendPlay(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-idle")
	server := startPlaybackMPVServerWithIdleState(t, socketPath, true)

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

	err := executeCommandForTest(t, "say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	cmds := server.commands()
	if len(cmds) != 2 {
		t.Fatalf("command count = %d, want 2", len(cmds))
	}
	if got := cmds[0]; len(got) < 2 || got[0] != "get_property" || got[1] != "idle-active" {
		t.Fatalf("first command = %#v, want get_property idle-active", got)
	}
	if got := cmds[1]; len(got) < 3 || got[0] != "loadfile" || got[2] != "append-play" {
		t.Fatalf("second command = %#v, want loadfile append-play", got)
	}
}

func TestSayUsesWAVForChatterboxEngine(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		Station: config.StationConfig{
			StateDir: t.TempDir(),
		},
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			DataDir: t.TempDir(),
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	if !strings.HasSuffix(ttsClient.lastOutPath, ".wav") {
		t.Fatalf("out path = %q, want .wav suffix", ttsClient.lastOutPath)
	}
}

func TestSayUsesAIFFForSystemSayEngine(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		Station: config.StationConfig{
			StateDir: t.TempDir(),
		},
		TTS: config.TTSConfig{
			Engine:         config.TTSEngineSystem,
			DataDir:        t.TempDir(),
			FallbackBinary: "/usr/bin/say",
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	if !strings.HasSuffix(ttsClient.lastOutPath, ".aiff") {
		t.Fatalf("out path = %q, want .aiff suffix", ttsClient.lastOutPath)
	}
}

func TestSayOnSuccessClearsPendingBanterCue(t *testing.T) {
	stateDir := t.TempDir()
	socketPath := makePlaybackSocketPath(t, "say-clear-pending")
	server := startPlaybackMPVServer(t, socketPath)

	store := station.NewAgentEventStore(stateDir)
	if err := store.SavePendingBanter(station.PendingBanter{
		EventID: "evt_123",
		NextSong: station.AgentSong{
			Seed:   "SZA - Saturn",
			Artist: "SZA",
			Title:  "Saturn",
		},
	}); err != nil {
		t.Fatalf("save pending banter: %v", err)
	}

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
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

	err := executeCommandForTest(t, "say", "hello")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	pending, err := store.LoadPendingBanter()
	if err != nil {
		t.Fatalf("load pending banter: %v", err)
	}
	if pending != nil {
		data, _ := json.Marshal(pending)
		t.Fatalf("pending banter should be cleared, got %s", string(data))
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
