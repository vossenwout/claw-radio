package cmd

import (
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

func TestSayVoiceNameResolvesToDataDirVoiceFile(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-voice-name")
	server := startPlaybackMPVServer(t, socketPath)

	dataDir := t.TempDir()
	voicePath := filepath.Join(dataDir, "voices", "pop.wav")
	if err := os.MkdirAll(filepath.Dir(voicePath), 0o755); err != nil {
		t.Fatalf("mkdir voices dir: %v", err)
	}
	if err := os.WriteFile(voicePath, []byte("voice"), 0o644); err != nil {
		t.Fatalf("write voice file: %v", err)
	}

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		TTS: config.TTSConfig{
			DataDir: dataDir,
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "hello", "--voice", "pop")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	if ttsClient.lastVoicePath != voicePath {
		t.Fatalf("voice path = %q, want %q", ttsClient.lastVoicePath, voicePath)
	}
}

func TestSayVoiceAbsolutePathPassesThrough(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-voice-absolute")
	server := startPlaybackMPVServer(t, socketPath)

	dataDir := t.TempDir()
	literalVoicePath := filepath.Join(t.TempDir(), "voice.wav")
	if err := os.WriteFile(literalVoicePath, []byte("voice"), 0o644); err != nil {
		t.Fatalf("write voice file: %v", err)
	}

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		TTS: config.TTSConfig{
			DataDir: dataDir,
		},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "hello", "--voice", literalVoicePath)
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	if ttsClient.lastVoicePath != literalVoicePath {
		t.Fatalf("voice path = %q, want %q", ttsClient.lastVoicePath, literalVoicePath)
	}
}

func TestSayUnknownVoiceFallsBackToDefaultVoice(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-voice-default")
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

	err := executeCommandForTest(t, "say", "hello", "--voice", "unknown")
	if err != nil {
		t.Fatalf("say failed: %v", err)
	}
	server.wait(t)

	if ttsClient.lastVoicePath != "" {
		t.Fatalf("voice path = %q, want empty string", ttsClient.lastVoicePath)
	}
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
}

func TestSayForPendingEventMarksFulfilled(t *testing.T) {
	stateDir := t.TempDir()
	socketPath := makePlaybackSocketPath(t, "say-for-event")
	server := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
		Station: config.StationConfig{
			StateDir: stateDir,
		},
		TTS: config.TTSConfig{DataDir: t.TempDir()},
	}
	store := station.NewAgentEventStore(stateDir)
	if err := store.SavePendingBanter(station.PendingBanter{
		EventID: "evt_123",
		NextSong: station.AgentSong{
			Artist: "Kendrick Lamar",
			Title:  "Money Trees",
			Path:   "/tmp/song.opus",
		},
	}); err != nil {
		t.Fatalf("save pending banter: %v", err)
	}

	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "banter", "--for", "evt_123")
	if err != nil {
		t.Fatalf("say --for failed: %v", err)
	}
	server.wait(t)

	pending, err := store.LoadPendingBanter()
	if err != nil {
		t.Fatalf("load pending banter: %v", err)
	}
	if pending == nil || !pending.Fulfilled {
		t.Fatalf("pending banter not fulfilled: %#v", pending)
	}
}

func TestSayForMissingPendingEventExitsOne(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		Station: config.StationConfig{StateDir: t.TempDir()},
		TTS:     config.TTSConfig{DataDir: t.TempDir()},
	}
	ttsClient := &fakeSayTTSClient{}
	restore := withSayTestHooks(cfg, ttsClient)
	defer restore()

	err := executeCommandForTest(t, "say", "banter", "--for", "evt_missing")
	assertExitCode(t, err, 1)
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
	origVoiceFlag := sayVoiceFlag
	origForFlag := sayForFlag

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
	sayVoiceFlag = ""
	sayForFlag = ""

	return func() {
		loadConfigFn = origLoad
		dialPlaybackClientFn = origDial
		newSayTTSClientFn = origTTS
		nowUnixNanoFn = origNow
		sayVoiceFlag = origVoiceFlag
		sayForFlag = origForFlag
	}
}
