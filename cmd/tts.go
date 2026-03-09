package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
)

const (
	pythonMissingMessage      = "Python 3.11 not found.\nIf you use pyenv, switch first:  pyenv shell 3.11\nInstall on macOS:  brew install python@3.11\nInstall on Linux:  apt install python3.11 python3.11-venv"
	ffmpegMissingMessage      = "ffmpeg not found.\nInstall on macOS:  brew install ffmpeg\nInstall on Linux:  apt install ffmpeg   (Debian/Ubuntu)\n                   dnf install ffmpeg   (Fedora)"
	chatterboxPackageVersion  = "0.1.6"
	ttsVoiceComingSoonMessage = "Custom TTS voices are planned, but disabled for v1 while voice-cloning support is finalized."
)

var (
	embeddedTTSFS fs.FS

	ttsLookPathFn    = exec.LookPath
	ttsExecCommandFn = exec.Command
	ttsReadFileFn    = fs.ReadFile
	ttsReadDirFn     = fs.ReadDir
	ttsWriteFileFn   = os.WriteFile
	ttsMkdirAllFn    = os.MkdirAll
	ttsRenameFn      = os.Rename
	ttsRemoveFn      = os.Remove
	ttsRemoveAllFn   = os.RemoveAll
	ttsStatFn        = os.Stat

	ttsVoiceNameFlag string
)

var invalidVoiceNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

var ttsCmd = &cobra.Command{
	Use:   "tts",
	Short: "Set up and manage radio voices",
}

var ttsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Set up voice generation for say",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSInstall(cmd)
	},
}

var ttsUseCmd = &cobra.Command{
	Use:       "use <chatterbox|system>",
	Short:     "Choose the active TTS engine",
	Args:      cobra.ExactValidArgs(1),
	ValidArgs: []string{config.TTSEngineChatterbox, config.TTSEngineSystem},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSUse(cmd, args[0])
	},
}

var ttsVoiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Manage saved voice samples",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSVoiceComingSoon(cmd)
	},
}

var ttsVoiceAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Save a new voice sample from a URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSVoiceComingSoon(cmd)
	},
}

// SetEmbeddedTTSFS injects the embedded tts assets filesystem from main.
func SetEmbeddedTTSFS(fsys fs.FS) {
	embeddedTTSFS = fsys
}

func runTTSInstall(cmd *cobra.Command) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pythonPath, err := resolveSupportedTTSPython()
	if err != nil {
		return exitCode(err, 4)
	}

	if err := ttsMkdirAllFn(cfg.TTS.DataDir, 0o755); err != nil {
		return fmt.Errorf("create tts data dir: %w", err)
	}

	venvDir := filepath.Join(cfg.TTS.DataDir, "venv")
	if err := ttsRemoveAllFn(venvDir); err != nil {
		return fmt.Errorf("remove existing python venv: %w", err)
	}
	if err := runTTSCommand(pythonPath, "-m", "venv", venvDir); err != nil {
		return fmt.Errorf("create python venv: %w", err)
	}

	if err := extractBundledTTSAssets(cfg.TTS.DataDir); err != nil {
		return fmt.Errorf("extract bundled tts assets: %w", err)
	}

	pipPath := filepath.Join(venvDir, "bin", "pip")
	if err := runTTSCommand(pipPath, "install", chatterboxPackageSpec()); err != nil {
		return fmt.Errorf("install python dependencies: %w", err)
	}
	if err := config.SetTTSEngine(config.TTSEngineChatterbox); err != nil {
		return fmt.Errorf("activate chatterbox engine: %w", err)
	}

	venvPython := filepath.Join(venvDir, "bin", "python")
	device, err := detectTorchDevice(venvPython)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Could not detect torch accelerator - defaulting to CPU until runtime proves otherwise")
	} else if device == "cpu" {
		fmt.Fprintln(cmd.ErrOrStderr(), "No CUDA or MPS accelerator detected - using CPU (slow on Linux VPS)")
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Chatterbox TTS installed and selected. Use: claw-radio say \"<text>\"")
	return nil
}

func runTTSUse(cmd *cobra.Command, engine string) error {
	parsed, ok := config.ParseTTSEngine(engine)
	if !ok {
		return fmt.Errorf("invalid tts engine %q", engine)
	}
	if err := config.SetTTSEngine(parsed); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "TTS engine set to %s\n", parsed)
	return nil
}

func runTTSVoiceComingSoon(cmd *cobra.Command) error {
	fmt.Fprintln(cmd.OutOrStdout(), ttsVoiceComingSoonMessage)
	return nil
}

func runTTSVoiceAdd(cmd *cobra.Command, url string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ytdlpPath, err := resolveRequiredBinary(cfg.YtDlp.Binary, "yt-dlp")
	if err != nil {
		return exitCode(errors.New(ytdlpMissingMessage), 4)
	}

	ffmpegPath, err := resolveRequiredBinary(cfg.FFmpeg.Binary, "ffmpeg")
	if err != nil {
		return exitCode(errors.New(ffmpegMissingMessage), 4)
	}

	voicesDir := filepath.Join(cfg.TTS.DataDir, "voices")
	if err := ttsMkdirAllFn(voicesDir, 0o755); err != nil {
		return fmt.Errorf("create voices dir: %w", err)
	}

	voiceName := sanitizeVoiceName(ttsVoiceNameFlag)
	if voiceName == "" {
		inferred, inferErr := inferVoiceName(ytdlpPath, url)
		if inferErr != nil {
			return fmt.Errorf("infer voice name: %w", inferErr)
		}
		voiceName = sanitizeVoiceName(inferred)
	}
	if voiceName == "" {
		voiceName = "voice"
	}

	outTemplate := filepath.Join(voicesDir, voiceName+".%(ext)s")
	if err := runTTSCommand(ytdlpPath, "-x", "--audio-format", "wav", "-o", outTemplate, url); err != nil {
		return fmt.Errorf("download voice sample: %w", err)
	}

	voicePath := filepath.Join(voicesDir, voiceName+".wav")
	if _, err := ttsStatFn(voicePath); err != nil {
		return fmt.Errorf("voice file not found after download: %w", err)
	}

	trimmedPath := filepath.Join(voicesDir, voiceName+".trim.wav")
	if err := runTTSCommand(ffmpegPath, "-i", voicePath, "-t", "30", "-y", trimmedPath); err != nil {
		return fmt.Errorf("trim voice sample: %w", err)
	}

	if err := replaceFile(trimmedPath, voicePath); err != nil {
		return fmt.Errorf("finalize trimmed voice sample: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Voice '%s' saved -> %s\n", voiceName, voicePath)
	return nil
}

func resolveRequiredBinary(configuredPath, fallbackName string) (string, error) {
	if path := strings.TrimSpace(configuredPath); path != "" {
		return path, nil
	}

	path, err := ttsLookPathFn(fallbackName)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("binary path empty")
	}
	return path, nil
}

func resolveSupportedTTSPython() (string, error) {
	candidates := []string{"python3.11", "python3"}
	seen := make(map[string]struct{}, len(candidates))
	unsupported := make([]string, 0)

	for _, candidate := range candidates {
		path, err := ttsLookPathFn(candidate)
		if err != nil || strings.TrimSpace(path) == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		version, err := readPythonVersion(path)
		if err != nil {
			unsupported = append(unsupported, fmt.Sprintf("%s (version check failed)", path))
			continue
		}
		if isSupportedTTSPython(version) {
			return path, nil
		}
		unsupported = append(unsupported, fmt.Sprintf("%s (%s)", path, version))
	}

	if len(unsupported) == 0 {
		return "", errors.New(pythonMissingMessage)
	}

	return "", fmt.Errorf(
		"custom TTS requires Python 3.11 for %s. Found unsupported interpreters: %s.\nIf you use pyenv, switch first:  pyenv shell 3.11\nInstall on macOS:  brew install python@3.11\nInstall on Linux:  apt install python3.11 python3.11-venv",
		chatterboxPackageSpec(),
		strings.Join(unsupported, ", "),
	)
}

func readPythonVersion(pythonPath string) (string, error) {
	out, err := runTTSCommandWithOutput(pythonPath, "-c", "import sys; print(f'{sys.version_info[0]}.{sys.version_info[1]}.{sys.version_info[2]}')")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isSupportedTTSPython(version string) bool {
	return strings.HasPrefix(strings.TrimSpace(version), "3.11.")
}

func chatterboxPackageSpec() string {
	return "chatterbox-tts==" + chatterboxPackageVersion
}

func runTTSCommand(command string, args ...string) error {
	_, err := runTTSCommandWithOutput(command, args...)
	return err
}

func runTTSCommandWithOutput(command string, args ...string) ([]byte, error) {
	out, err := ttsExecCommandFn(command, args...).CombinedOutput()
	if err == nil {
		return out, nil
	}

	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return out, fmt.Errorf("%s: %w", filepath.Base(command), err)
	}
	return out, fmt.Errorf("%s: %w: %s", filepath.Base(command), err, msg)
}

func extractBundledTTSAssets(dataDir string) error {
	if embeddedTTSFS == nil {
		return errors.New("embedded tts assets are not configured")
	}

	daemonBytes, err := ttsReadFileFn(embeddedTTSFS, "tts/daemon.py")
	if err != nil {
		return fmt.Errorf("read tts/daemon.py: %w", err)
	}
	if err := writeFileWithParents(filepath.Join(dataDir, "daemon.py"), daemonBytes); err != nil {
		return err
	}

	entries, err := ttsReadDirFn(embeddedTTSFS, "tts/voices")
	if err != nil {
		return fmt.Errorf("read tts/voices: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		sourcePath := "tts/voices/" + entry.Name()
		voiceBytes, readErr := ttsReadFileFn(embeddedTTSFS, sourcePath)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", sourcePath, readErr)
		}

		targetPath := filepath.Join(dataDir, "voices", entry.Name())
		if err := writeFileWithParents(targetPath, voiceBytes); err != nil {
			return err
		}
	}

	return nil
}

func writeFileWithParents(path string, data []byte) error {
	if err := ttsMkdirAllFn(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", path, err)
	}
	if err := ttsWriteFileFn(path, data, 0o644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

func inferVoiceName(ytdlpPath, url string) (string, error) {
	out, err := runTTSCommandWithOutput(ytdlpPath, "--print", "title", "--no-playlist", url)
	if err != nil {
		return "", err
	}

	title := strings.TrimSpace(string(out))
	if title == "" {
		return "", errors.New("yt-dlp returned empty title")
	}
	return title, nil
}

func sanitizeVoiceName(name string) string {
	sanitized := strings.ToLower(strings.TrimSpace(name))
	sanitized = invalidVoiceNameChars.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")
	return sanitized
}

func replaceFile(src, dst string) error {
	if err := ttsRemoveFn(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return ttsRenameFn(src, dst)
}

func detectTorchDevice(pythonPath string) (string, error) {
	out, err := runTTSCommandWithOutput(pythonPath, "-c", "import torch; print('mps' if torch.backends.mps.is_available() else ('cuda' if torch.cuda.is_available() else 'cpu'))")
	if err != nil {
		return "", err
	}
	device := strings.TrimSpace(string(out))
	switch device {
	case "mps", "cuda", "cpu":
		return device, nil
	default:
		return "", fmt.Errorf("unexpected torch device %q", device)
	}
}

func init() {
	ttsVoiceAddCmd.Flags().StringVar(&ttsVoiceNameFlag, "name", "", "Voice profile name")

	ttsCmd.AddCommand(ttsUseCmd)
	ttsVoiceCmd.AddCommand(ttsVoiceAddCmd)
	ttsCmd.AddCommand(ttsInstallCmd)
	ttsCmd.AddCommand(ttsVoiceCmd)
	RootCmd.AddCommand(ttsCmd)
}
