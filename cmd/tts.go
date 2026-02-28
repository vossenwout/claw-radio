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
)

const (
	pythonMissingMessage = "python3 not found.\nInstall on macOS:  brew install python\nInstall on Linux:  apt install python3 python3-venv"
	ffmpegMissingMessage = "ffmpeg not found.\nInstall on macOS:  brew install ffmpeg\nInstall on Linux:  apt install ffmpeg   (Debian/Ubuntu)\n                   dnf install ffmpeg   (Fedora)"
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
	ttsStatFn        = os.Stat

	ttsVoiceNameFlag string
)

var invalidVoiceNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

var ttsCmd = &cobra.Command{
	Use:   "tts",
	Short: "Install and manage TTS voices",
}

var ttsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Chatterbox TTS into the data directory",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSInstall(cmd)
	},
}

var ttsVoiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Manage voice prompt files",
}

var ttsVoiceAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Download and store a voice prompt WAV",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTTSVoiceAdd(cmd, args[0])
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

	pythonPath, err := resolveRequiredBinary("", "python3")
	if err != nil {
		return exitCode(errors.New(pythonMissingMessage), 4)
	}

	if err := ttsMkdirAllFn(cfg.TTS.DataDir, 0o755); err != nil {
		return fmt.Errorf("create tts data dir: %w", err)
	}

	venvDir := filepath.Join(cfg.TTS.DataDir, "venv")
	if err := runTTSCommand(pythonPath, "-m", "venv", venvDir); err != nil {
		return fmt.Errorf("create python venv: %w", err)
	}

	if err := extractBundledTTSAssets(cfg.TTS.DataDir); err != nil {
		return fmt.Errorf("extract bundled tts assets: %w", err)
	}

	pipPath := filepath.Join(venvDir, "bin", "pip")
	if err := runTTSCommand(pipPath, "install", "chatterbox-tts", "torch", "torchaudio"); err != nil {
		return fmt.Errorf("install python dependencies: %w", err)
	}

	venvPython := filepath.Join(venvDir, "bin", "python")
	cudaAvailable, err := cudaAvailable(venvPython)
	if err != nil || !cudaAvailable {
		fmt.Fprintln(cmd.ErrOrStderr(), "CUDA not available - using CPU (slow on Linux VPS)")
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Chatterbox TTS installed. Use: claw-radio say \"<text>\"")
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

func cudaAvailable(pythonPath string) (bool, error) {
	out, err := runTTSCommandWithOutput(pythonPath, "-c", "import torch; print('1' if torch.cuda.is_available() else '0')")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

func init() {
	ttsVoiceAddCmd.Flags().StringVar(&ttsVoiceNameFlag, "name", "", "Voice profile name")

	ttsVoiceCmd.AddCommand(ttsVoiceAddCmd)
	ttsCmd.AddCommand(ttsInstallCmd)
	ttsCmd.AddCommand(ttsVoiceCmd)
	RootCmd.AddCommand(ttsCmd)
}
