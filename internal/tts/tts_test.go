package tts

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestRenderWarmDaemonReturnsNilWithoutFallback(t *testing.T) {
	setMockExecCommand(t)
	t.Setenv("TTS_HELPER_EXIT", "99")

	socketPath := makeSocketPath(t, "warm")
	requests := startMockDaemon(t, socketPath, map[string]interface{}{"status": "ok"})

	dataDir := t.TempDir()
	_ = ensureVenvPython(t, dataDir)

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:         config.TTSEngineChatterbox,
			Socket:         socketPath,
			DataDir:        dataDir,
			FallbackBinary: "espeak-ng",
		},
	})

	if err := client.Render("hello from daemon", "", filepath.Join(t.TempDir(), "out.wav")); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	req := <-requests
	if req["text"] != "hello from daemon" {
		t.Fatalf("request text = %#v, want %#v", req["text"], "hello from daemon")
	}
}

func TestRenderFallsBackToOneShotWhenDaemonConnectionRefusedAndVenvPresent(t *testing.T) {
	setMockExecCommand(t)

	socketPath := makeRefusedSocketPath(t, "oneshot")
	dataDir := t.TempDir()
	pythonPath := ensureVenvPython(t, dataDir)
	outPath := filepath.Join(t.TempDir(), "speech.wav")
	t.Setenv("TTS_HELPER_LOG", filepath.Join(t.TempDir(), "tts-helper.log"))

	t.Setenv("TTS_HELPER_EXIT", "0")

	client := NewClient(&config.Config{
		FFmpeg: config.BinaryConfig{Binary: "ffmpeg"},
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			Socket:  socketPath,
			DataDir: dataDir,
		},
	})

	if err := client.Render("hello one-shot", "", outPath); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	logData, err := os.ReadFile(os.Getenv("TTS_HELPER_LOG"))
	if err != nil {
		t.Fatalf("ReadFile(helper log) error: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, pythonPath+" "+filepath.Join(dataDir, "daemon.py")+" --one-shot hello one-shot "+outPath) {
		t.Fatalf("helper log missing one-shot call: %q", log)
	}
	if !strings.Contains(log, "ffmpeg -y -i "+outPath+" -af "+chatterboxPostProcessFilter()+" "+filepath.Join(filepath.Dir(outPath), "speech.post.wav")) {
		t.Fatalf("helper log missing ffmpeg post-process call: %q", log)
	}
}

func TestRenderFallsBackToSystemTTSWhenNoVenv(t *testing.T) {
	setMockExecCommand(t)

	socketPath := makeRefusedSocketPath(t, "system")
	dataDir := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "speech.wav")

	t.Setenv("TTS_HELPER_EXPECT_CMD", "espeak-ng")
	t.Setenv("TTS_HELPER_EXPECT_ARGS", strings.Join([]string{
		"-w",
		outPath,
		"hello system",
	}, "\x1f"))
	t.Setenv("TTS_HELPER_EXIT", "0")

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:         config.TTSEngineSystem,
			Socket:         socketPath,
			DataDir:        dataDir,
			FallbackBinary: "espeak-ng",
		},
	})

	if err := client.Render("hello system", "", outPath); err != nil {
		t.Fatalf("Render() error: %v", err)
	}
}

func TestRenderReturnsHelpfulErrorWhenNoFallbackBinary(t *testing.T) {
	socketPath := makeRefusedSocketPath(t, "nobinary")
	dataDir := t.TempDir()

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			Socket:  socketPath,
			DataDir: dataDir,
		},
	})

	err := client.Render("hello", "", filepath.Join(t.TempDir(), "out.wav"))
	if err == nil {
		t.Fatal("Render() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Chatterbox TTS not installed") {
		t.Fatalf("Render() error = %q, want contains %q", err.Error(), "Chatterbox TTS not installed")
	}
}

func TestRenderChatterboxSkipsPostProcessWhenFFmpegMissing(t *testing.T) {
	setMockExecCommand(t)

	socketPath := makeRefusedSocketPath(t, "oneshot-no-ffmpeg")
	dataDir := t.TempDir()
	pythonPath := ensureVenvPython(t, dataDir)
	outPath := filepath.Join(t.TempDir(), "speech.wav")
	t.Setenv("TTS_HELPER_LOG", filepath.Join(t.TempDir(), "tts-helper.log"))
	t.Setenv("TTS_HELPER_EXIT", "0")

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			Socket:  socketPath,
			DataDir: dataDir,
		},
	})

	if err := client.Render("hello one-shot", "", outPath); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	logData, err := os.ReadFile(os.Getenv("TTS_HELPER_LOG"))
	if err != nil {
		t.Fatalf("ReadFile(helper log) error: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, pythonPath+" "+filepath.Join(dataDir, "daemon.py")+" --one-shot hello one-shot "+outPath) {
		t.Fatalf("helper log missing one-shot call: %q", log)
	}
	if strings.Contains(log, "ffmpeg ") {
		t.Fatalf("helper log should not include ffmpeg when binary missing: %q", log)
	}
}

func TestRenderWarmDaemonIncludesVoicePromptWhenProvided(t *testing.T) {
	socketPath := makeSocketPath(t, "voice-on")
	requests := startMockDaemon(t, socketPath, map[string]interface{}{"status": "ok"})

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			Socket:  socketPath,
			DataDir: t.TempDir(),
		},
	})

	voicePath := filepath.Join(t.TempDir(), "voice.wav")
	if err := client.Render("with voice", voicePath, filepath.Join(t.TempDir(), "out.wav")); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	req := <-requests
	got, ok := req["voice_prompt"]
	if !ok {
		t.Fatalf("request missing voice_prompt: %#v", req)
	}
	if got != voicePath {
		t.Fatalf("voice_prompt = %#v, want %#v", got, voicePath)
	}
}

func TestRenderWarmDaemonOmitsVoicePromptWhenEmpty(t *testing.T) {
	socketPath := makeSocketPath(t, "voice-off")
	requests := startMockDaemon(t, socketPath, map[string]interface{}{"status": "ok"})

	client := NewClient(&config.Config{
		TTS: config.TTSConfig{
			Engine:  config.TTSEngineChatterbox,
			Socket:  socketPath,
			DataDir: t.TempDir(),
		},
	})

	if err := client.Render("without voice", "", filepath.Join(t.TempDir(), "out.wav")); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	req := <-requests
	if _, ok := req["voice_prompt"]; ok {
		t.Fatalf("request unexpectedly includes voice_prompt: %#v", req["voice_prompt"])
	}
}

func TestOutputExtensionUsesWAVForChatterbox(t *testing.T) {
	got := OutputExtension(&config.Config{TTS: config.TTSConfig{Engine: config.TTSEngineChatterbox, FallbackBinary: "say"}})
	if got != ".wav" {
		t.Fatalf("OutputExtension() = %q, want %q", got, ".wav")
	}
}

func TestOutputExtensionUsesAIFFForSystemSay(t *testing.T) {
	got := OutputExtension(&config.Config{TTS: config.TTSConfig{Engine: config.TTSEngineSystem, FallbackBinary: "/usr/bin/say"}})
	if got != ".aiff" {
		t.Fatalf("OutputExtension() = %q, want %q", got, ".aiff")
	}
}

func setMockExecCommand(t *testing.T) {
	t.Helper()

	orig := execCommand
	execCommand = helperCommand
	t.Cleanup(func() {
		execCommand = orig
	})

	t.Setenv("TTS_HELPER_EXPECT_CMD", "")
	t.Setenv("TTS_HELPER_EXPECT_ARGS", "")
	t.Setenv("TTS_HELPER_EXIT", "0")
	t.Setenv("TTS_HELPER_STDOUT", "")
	t.Setenv("TTS_HELPER_STDERR", "")
	t.Setenv("TTS_HELPER_LOG", "")
}

func helperCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestTTSHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_TTS_HELPER=1")
	return cmd
}

func TestTTSHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_TTS_HELPER") != "1" {
		return
	}

	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+1 >= len(args) {
		fmt.Fprint(os.Stderr, "missing helper separator")
		os.Exit(2)
	}

	gotCmd := args[sep+1]
	gotArgs := args[sep+2:]
	appendHelperLog(gotCmd, gotArgs)

	if wantCmd := os.Getenv("TTS_HELPER_EXPECT_CMD"); wantCmd != "" && gotCmd != wantCmd {
		fmt.Fprintf(os.Stderr, "unexpected command: got %q want %q", gotCmd, wantCmd)
		os.Exit(3)
	}

	if wantArgsRaw := os.Getenv("TTS_HELPER_EXPECT_ARGS"); wantArgsRaw != "" {
		wantArgs := strings.Split(wantArgsRaw, "\x1f")
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			fmt.Fprintf(os.Stderr, "unexpected args: got %#v want %#v", gotArgs, wantArgs)
			os.Exit(4)
		}
	}

	fmt.Fprint(os.Stdout, os.Getenv("TTS_HELPER_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("TTS_HELPER_STDERR"))

	if filepath.Base(gotCmd) == "ffmpeg" {
		outPath := gotArgs[len(gotArgs)-1]
		if err := os.WriteFile(outPath, []byte("processed"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write ffmpeg output: %v", err)
			os.Exit(6)
		}
	}

	code := 0
	if raw := os.Getenv("TTS_HELPER_EXIT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad TTS_HELPER_EXIT: %q", raw)
			os.Exit(5)
		}
		code = parsed
	}
	os.Exit(code)
}

func appendHelperLog(command string, args []string) {
	path := os.Getenv("TTS_HELPER_LOG")
	if strings.TrimSpace(path) == "" {
		return
	}
	line := strings.TrimSpace(command + " " + strings.Join(args, " "))
	if line == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, line)
}

func startMockDaemon(t *testing.T, socketPath string, response map[string]interface{}) <-chan map[string]interface{} {
	t.Helper()

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen() error: %v", err)
	}

	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	})

	requests := make(chan map[string]interface{}, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept() error: %v", err)
			return
		}
		defer conn.Close()

		var req map[string]interface{}
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			t.Errorf("Decode() error: %v", err)
			return
		}
		requests <- req

		if err := json.NewEncoder(conn).Encode(response); err != nil {
			t.Errorf("Encode() error: %v", err)
		}
	}()

	return requests
}

func makeRefusedSocketPath(t *testing.T, prefix string) string {
	t.Helper()

	path := makeSocketPath(t, prefix)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("syscall.Socket() error: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Close(fd)
	})

	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		t.Fatalf("syscall.Bind() error: %v", err)
	}
	if err := syscall.Listen(fd, 1); err != nil {
		t.Fatalf("syscall.Listen() error: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("syscall.Close() error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket missing for %q: %v", path, err)
	}

	return path
}

func makeSocketPath(t *testing.T, prefix string) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/claw-radio-tts-%s-%d-%d.sock", prefix, os.Getpid(), time.Now().UnixNano())
	_ = os.Remove(path)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}

func ensureVenvPython(t *testing.T, dataDir string) string {
	t.Helper()

	pythonPath := filepath.Join(dataDir, "venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	return pythonPath
}
