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

func TestSayWhenMPVNotRunningExitsFive(t *testing.T) {
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

	err := executeCommandForTest(t, "say", "hello")
	assertExitCode(t, err, 5)
}

func TestSayWhenTTSUnavailableExitsFour(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "say-unavailable")
	server := startPlaybackMPVServer(t, socketPath)

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
	server.wait(t)
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

	return func() {
		loadConfigFn = origLoad
		dialPlaybackClientFn = origDial
		newSayTTSClientFn = origTTS
		nowUnixNanoFn = origNow
		sayVoiceFlag = origVoiceFlag
	}
}
