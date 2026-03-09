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
		return "", errors.New("not found")
	}

	err := executeCommandForTest(t, "tts", "install")
	assertExitCode(t, err, 4)
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "python 3.11") {
		t.Fatalf("error must mention python 3.11, got %q", err)
	}
	if !strings.Contains(msg, "pyenv shell 3.11") {
		t.Fatalf("error must mention pyenv switch command, got %q", err)
	}
}

func TestTTSInstallRejectsUnsupportedPythonVersion(t *testing.T) {
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
			return "python3-mock", nil
		}
		return "", errors.New("not found")
	}
	ttsExecCommandFn = ttsHelperCommand

	err := executeCommandForTest(t, "tts", "install")
	assertExitCode(t, err, 4)
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "python 3.11") {
		t.Fatalf("error must mention python 3.11, got %q", err)
	}
	if !strings.Contains(msg, "3.14.3") {
		t.Fatalf("error must mention discovered unsupported version, got %q", err)
	}
	if !strings.Contains(msg, "pyenv shell 3.11") {
		t.Fatalf("error must mention pyenv switch command, got %q", err)
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
		if name == "python3.11" {
			return "python3.11-mock", nil
		}
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

	markerPath := filepath.Join(dataDir, "venv", "python-source.txt")
	markerBytes, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("read python source marker: %v", readErr)
	}
	if strings.TrimSpace(string(markerBytes)) != "python3.11-mock" {
		t.Fatalf("expected installer to use python3.11-mock, got %q", strings.TrimSpace(string(markerBytes)))
	}
}

func TestTTSInstallPinsChatterboxVersion(t *testing.T) {
	dataDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tts-helper.log")
	t.Setenv("TTS_HELPER_LOG", logPath)

	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: dataDir},
	}
	assets := fstest.MapFS{
		"tts/daemon.py":      {Data: []byte("print('daemon ok')\n")},
		"tts/voices/pop.wav": {Data: []byte("RIFFpop")},
	}

	restore := withTTSTestHooks(cfg, assets)
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "python3.11" {
			return "python3.11-mock", nil
		}
		return "", errors.New("not found")
	}
	ttsExecCommandFn = ttsHelperCommand

	err := executeCommandForTest(t, "tts", "install")
	if err != nil {
		t.Fatalf("tts install failed: %v", err)
	}

	logBytes, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read helper log: %v", readErr)
	}
	log := string(logBytes)
	pipInstallLine := filepath.ToSlash(filepath.Join(dataDir, "venv", "bin", "pip")) + " install " + chatterboxPackageSpec()
	if !strings.Contains(log, pipInstallLine) {
		t.Fatalf("expected pip install to include %q, got %q", chatterboxPackageSpec(), log)
	}
	if strings.Contains(log, pipInstallLine+" torch") || strings.Contains(log, pipInstallLine+" torchaudio") {
		t.Fatalf("expected installer to rely on chatterbox package pins, got %q", log)
	}
}

func TestTTSInstallActivatesChatterboxEngine(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CLAW_RADIO_CONFIG", configPath)

	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: dataDir},
	}
	assets := fstest.MapFS{
		"tts/daemon.py":      {Data: []byte("print('daemon ok')\n")},
		"tts/voices/pop.wav": {Data: []byte("RIFFpop")},
	}

	restore := withTTSTestHooks(cfg, assets)
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "python3.11" {
			return "python3.11-mock", nil
		}
		return "", errors.New("not found")
	}
	ttsExecCommandFn = ttsHelperCommand

	err := executeCommandForTest(t, "tts", "install")
	if err != nil {
		t.Fatalf("tts install failed: %v", err)
	}

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config after install: %v", readErr)
	}
	if !strings.Contains(string(data), `"engine": "chatterbox"`) {
		t.Fatalf("expected install to activate chatterbox engine, got %s", string(data))
	}
}

func TestTTSInstallRecreatesExistingVenv(t *testing.T) {
	dataDir := t.TempDir()
	stalePath := filepath.Join(dataDir, "venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("create stale venv dir: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("stale-python"), 0o644); err != nil {
		t.Fatalf("write stale venv file: %v", err)
	}

	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: dataDir},
	}
	assets := fstest.MapFS{
		"tts/daemon.py":      {Data: []byte("print('daemon ok')\n")},
		"tts/voices/pop.wav": {Data: []byte("RIFFpop")},
	}

	restore := withTTSTestHooks(cfg, assets)
	defer restore()

	ttsLookPathFn = func(name string) (string, error) {
		if name == "python3.11" {
			return "python3.11-mock", nil
		}
		return "", errors.New("not found")
	}
	ttsExecCommandFn = ttsHelperCommand

	err := executeCommandForTest(t, "tts", "install")
	if err != nil {
		t.Fatalf("tts install failed: %v", err)
	}

	if _, statErr := os.Stat(stalePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected stale venv file to be removed, got %v", statErr)
	}

	markerPath := filepath.Join(dataDir, "venv", "python-source.txt")
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("expected recreated venv marker at %s: %v", markerPath, statErr)
	}
}

func TestTTSVoicePrintsComingSoonMessage(t *testing.T) {
	cfg := &config.Config{
		TTS: config.TTSConfig{DataDir: t.TempDir()},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("tts", "voice")
	if err != nil {
		t.Fatalf("tts voice failed: %v", err)
	}
	if !strings.Contains(stdout, ttsVoiceComingSoonMessage) {
		t.Fatalf("stdout missing coming soon message, got %q", stdout)
	}
}

func TestTTSVoiceAddPrintsComingSoonMessageWithoutCreatingVoice(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		TTS:    config.TTSConfig{DataDir: dataDir},
		YtDlp:  config.BinaryConfig{Binary: "yt-dlp-mock"},
		FFmpeg: config.BinaryConfig{Binary: "ffmpeg-mock"},
	}

	restore := withTTSTestHooks(cfg, fstest.MapFS{})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("tts", "voice", "add", "https://example.com/voice", "--name", "country")
	if err != nil {
		t.Fatalf("tts voice add failed: %v", err)
	}
	if !strings.Contains(stdout, ttsVoiceComingSoonMessage) {
		t.Fatalf("stdout missing coming soon message, got %q", stdout)
	}

	voicePath := filepath.Join(dataDir, "voices", "country.wav")
	if _, statErr := os.Stat(voicePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no voice output at %s, got err=%v", voicePath, statErr)
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

func TestTTSUseWritesSelectedEngine(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CLAW_RADIO_CONFIG", configPath)

	err, stdout, _ := executeCommandWithOutputForTest("tts", "use", "system")
	if err != nil {
		t.Fatalf("tts use failed: %v", err)
	}
	if !strings.Contains(stdout, "TTS engine set to system") {
		t.Fatalf("stdout missing confirmation, got %q", stdout)
	}

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(data), `"engine": "system"`) {
		t.Fatalf("expected config to contain system engine, got %s", string(data))
	}
}

func withTTSTestHooks(cfg *config.Config, assets fs.FS) func() {
	origLoad := loadConfigFn
	origLookPath := ttsLookPathFn
	origExec := ttsExecCommandFn
	origEmbedded := embeddedTTSFS
	origVoiceName := ttsVoiceNameFlag
	origRemoveAll := ttsRemoveAllFn

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	ttsLookPathFn = exec.LookPath
	ttsExecCommandFn = exec.Command
	ttsRemoveAllFn = os.RemoveAll
	SetEmbeddedTTSFS(assets)
	ttsVoiceNameFlag = ""

	return func() {
		loadConfigFn = origLoad
		ttsLookPathFn = origLookPath
		ttsExecCommandFn = origExec
		ttsRemoveAllFn = origRemoveAll
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
	case command == "python3-mock", command == "python3.11-mock":
		handlePythonHelper(command, commandArgs)
	case strings.HasSuffix(filepath.ToSlash(command), "/pip"):
		appendTTSHelperLog(command, commandArgs)
		os.Exit(0)
	case strings.HasSuffix(filepath.ToSlash(command), "/python"):
		appendTTSHelperLog(command, commandArgs)
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

func handlePythonHelper(command string, args []string) {
	appendTTSHelperLog(command, args)
	if len(args) >= 2 && args[0] == "-c" {
		switch command {
		case "python3.11-mock":
			fmt.Fprint(os.Stdout, "3.11.14\n")
		default:
			fmt.Fprint(os.Stdout, "3.14.3\n")
		}
		os.Exit(0)
	}
	if len(args) >= 3 && args[0] == "-m" && args[1] == "venv" {
		venvDir := args[2]
		if err := os.MkdirAll(filepath.Join(venvDir, "bin"), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create venv dir: %v", err)
			os.Exit(10)
		}
		if err := os.WriteFile(filepath.Join(venvDir, "python-source.txt"), []byte(command), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write python source marker: %v", err)
			os.Exit(11)
		}
		os.Exit(0)
	}
	os.Exit(0)
}

func appendTTSHelperLog(command string, args []string) {
	path := strings.TrimSpace(os.Getenv("TTS_HELPER_LOG"))
	if path == "" {
		return
	}
	line := strings.TrimSpace(command + " " + strings.Join(args, " "))
	if line == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open helper log: %v", err)
		os.Exit(40)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil {
		fmt.Fprintf(os.Stderr, "write helper log: %v", err)
		os.Exit(41)
	}
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
