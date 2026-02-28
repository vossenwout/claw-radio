package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestTTSInstallWithoutPythonExitsFour(t *testing.T) {
	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: t.TempDir()},
	}
	assets := fstest.MapFS{
		"tts/daemon.py":      {Data: []byte("daemon")},
		"tts/voices/pop.wav": {Data: []byte("RIFFpop")},
	}

	restore := withTTSTestHooks(cfg, assets)
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "python3" {
			return "", errors.New("not found")
		}
		return "", errors.New("not found")
	}

	err := executeCommandForTest(t, "tts", "install")
	assertExitCode(t, err, 4)
	if !strings.Contains(strings.ToLower(err.Error()), "python3") {
		t.Fatalf("error must mention python3, got %q", err)
	}
}

func TestTTSInstallExtractsDaemonAndBundledVoice(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: dataDir},
	}
	assets := fstest.MapFS{
		"tts/daemon.py":          {Data: []byte("print('daemon ok')\n")},
		"tts/voices/pop.wav":     {Data: []byte("RIFFpop")},
		"tts/voices/country.wav": {Data: []byte("RIFFcountry")},
	}

	restore := withTTSTestHooks(cfg, assets)
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "python3" {
			return "python3-mock", nil
		}
		return "", errors.New("not found")
	}
	ttsExecCommandFn = ttsHelperCommand

	err := executeCommandForTest(t, "tts", "install")
	if err != nil {
		t.Fatalf("tts install failed: %v", err)
	}

	daemonPath := filepath.Join(dataDir, "daemon.py")
	daemonBytes, readErr := os.ReadFile(daemonPath)
	if readErr != nil {
		t.Fatalf("read daemon.py: %v", readErr)
	}
	if string(daemonBytes) != "print('daemon ok')\n" {
		t.Fatalf("daemon.py content mismatch: got %q", string(daemonBytes))
	}

	popPath := filepath.Join(dataDir, "voices", "pop.wav")
	if _, statErr := os.Stat(popPath); statErr != nil {
		t.Fatalf("expected extracted pop voice at %s: %v", popPath, statErr)
	}
}

func TestTTSVoiceAddWhenYtDlpMissingExitsFour(t *testing.T) {
	cfg := &config.Config{
		TTS:    config.TTSConfig{DataDir: t.TempDir()},
		YtDlp:  config.BinaryConfig{Binary: ""},
		FFmpeg: config.BinaryConfig{Binary: "ffmpeg-mock"},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "yt-dlp" {
			return "", errors.New("not found")
		}
		if name == "ffmpeg" {
			return "ffmpeg-mock", nil
		}
		return "", errors.New("not found")
	}

	err := executeCommandForTest(t, "tts", "voice", "add", "https://example.com/voice")
	assertExitCode(t, err, 4)
	if !strings.Contains(strings.ToLower(err.Error()), "yt-dlp") {
		t.Fatalf("error must mention yt-dlp, got %q", err)
	}
}

func TestTTSVoiceAddWhenFFmpegMissingExitsFourWithInstallHint(t *testing.T) {
	cfg := &config.Config{
		TTS:    config.TTSConfig{DataDir: t.TempDir()},
		YtDlp:  config.BinaryConfig{Binary: "yt-dlp-mock"},
		FFmpeg: config.BinaryConfig{Binary: ""},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "ffmpeg" {
			return "", errors.New("not found")
		}
		if name == "yt-dlp" {
			return "yt-dlp-mock", nil
		}
		return "", errors.New("not found")
	}

	err := executeCommandForTest(t, "tts", "voice", "add", "https://example.com/voice")
	assertExitCode(t, err, 4)
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "ffmpeg") {
		t.Fatalf("error must mention ffmpeg, got %q", err)
	}
	if !strings.Contains(msg, "install") {
		t.Fatalf("error must include install instructions, got %q", err)
	}
}

func TestTTSVoiceAddDownloadsTrimsAndPrintsConfirmation(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		TTS:    config.TTSConfig{DataDir: dataDir},
		YtDlp:  config.BinaryConfig{Binary: "yt-dlp-mock"},
		FFmpeg: config.BinaryConfig{Binary: "ffmpeg-mock"},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	ttsExecCommandFn = ttsHelperCommand

	err, stdout, _ := executeCommandWithOutputForTest("tts", "voice", "add", "https://example.com/voice", "--name", "country")
	if err != nil {
		t.Fatalf("tts voice add failed: %v", err)
	}

	voicePath := filepath.Join(dataDir, "voices", "country.wav")
	if _, statErr := os.Stat(voicePath); statErr != nil {
		t.Fatalf("expected voice output at %s: %v", voicePath, statErr)
	}

	if !strings.Contains(stdout, "Voice 'country' saved") {
		t.Fatalf("stdout missing confirmation, got %q", stdout)
	}
}

func TestTTSVoiceAddWithoutURLExitsTwo(t *testing.T) {
	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: t.TempDir()},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("tts", "voice", "add")
	assertExitCode(t, err, 2)
	if !strings.Contains(stdout+stderr, "Usage:") {
		t.Fatalf("usage output missing: stdout=%q stderr=%q", stdout, stderr)
	}
}

func withTTSTestHooks(cfg *config.Config, assets fs.FS) func() {
	origLoad := loadConfigFn
	origLookPath := ttsLookPathFn
	origExec := ttsExecCommandFn
	origEmbedded := embeddedTTSFS
	origVoiceName := ttsVoiceNameFlag

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	ttsLookPathFn = exec.LookPath
	ttsExecCommandFn = exec.Command
	SetEmbeddedTTSFS(assets)
	ttsVoiceNameFlag = ""

	return func() {
		loadConfigFn = origLoad
		ttsLookPathFn = origLookPath
		ttsExecCommandFn = origExec
		embeddedTTSFS = origEmbedded
		ttsVoiceNameFlag = origVoiceName
	}
}

func ttsHelperCommand(command string, args ...string) *exec.Cmd {
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

	command := args[sep+1]
	commandArgs := args[sep+2:]

	switch {
	case command == "python3-mock":
		handlePythonHelper(commandArgs)
	case strings.HasSuffix(filepath.ToSlash(command), "/pip"):
		os.Exit(0)
	case strings.HasSuffix(filepath.ToSlash(command), "/python"):
		handleVenvPythonHelper(commandArgs)
	case command == "yt-dlp-mock":
		handleYtDlpHelper(commandArgs)
	case command == "ffmpeg-mock":
		handleFFmpegHelper(commandArgs)
	default:
		fmt.Fprintf(os.Stderr, "unexpected helper command: %s", command)
		os.Exit(3)
	}
}

func handlePythonHelper(args []string) {
	if len(args) >= 3 && args[0] == "-m" && args[1] == "venv" {
		venvDir := args[2]
		if err := os.MkdirAll(filepath.Join(venvDir, "bin"), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create venv dir: %v", err)
			os.Exit(10)
		}
		os.Exit(0)
	}
	os.Exit(0)
}

func handleVenvPythonHelper(args []string) {
	if len(args) >= 2 && args[0] == "-c" {
		fmt.Fprint(os.Stdout, "0\n")
	}
	os.Exit(0)
}

func handleYtDlpHelper(args []string) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-o" {
			out := strings.ReplaceAll(args[i+1], "%(ext)s", "wav")
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "mkdir voice dir: %v", err)
				os.Exit(20)
			}
			if err := os.WriteFile(out, []byte("wavdata"), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "write wav: %v", err)
				os.Exit(21)
			}
			break
		}
	}
	if len(args) >= 3 && args[0] == "--print" && args[1] == "title" {
		fmt.Fprint(os.Stdout, "Mock Voice Title")
	}
	os.Exit(0)
}

func handleFFmpegHelper(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "ffmpeg args missing")
		os.Exit(30)
	}
	out := args[len(args)-1]
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir trimmed output dir: %v", err)
		os.Exit(31)
	}
	if err := os.WriteFile(out, []byte("trimmed"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write trimmed wav: %v", err)
		os.Exit(32)
	}
	os.Exit(0)
}
