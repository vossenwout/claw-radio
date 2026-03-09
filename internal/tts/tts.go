package tts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/vossenwout/claw-radio/internal/config"
)

var execCommand = exec.Command

const chatterboxPostProcessGainDB = 8.0

type Client struct {
	cfg *config.Config
}

func NewClient(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

func OutputExtension(cfg *config.Config) string {
	if cfg == nil {
		return ".wav"
	}
	if config.NormalizeTTSEngine(cfg.TTS.Engine) == config.TTSEngineChatterbox {
		return ".wav"
	}
	if strings.EqualFold(filepath.Base(strings.TrimSpace(cfg.TTS.FallbackBinary)), "say") {
		return ".aiff"
	}
	return ".wav"
}

func (c *Client) Render(text, voicePath, outPath string) error {
	if c == nil || c.cfg == nil {
		return fmt.Errorf("tts client config is nil")
	}

	switch config.NormalizeTTSEngine(c.cfg.TTS.Engine) {
	case config.TTSEngineChatterbox:
		return c.renderChatterbox(text, voicePath, outPath)
	default:
		return c.renderSystem(text, outPath)
	}
}

func (c *Client) renderChatterbox(text, voicePath, outPath string) error {
	if err := c.renderDaemon(text, voicePath, outPath); err == nil {
		return c.postProcessChatterboxOutput(outPath)
	} else if !isDaemonUnavailable(err) {
		return err
	}

	if err := c.renderOneShot(text, voicePath, outPath); err == nil {
		return c.postProcessChatterboxOutput(outPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return fmt.Errorf("Chatterbox TTS not installed. Run: claw-radio tts install")
}

func (c *Client) postProcessChatterboxOutput(outPath string) error {
	ffmpeg := strings.TrimSpace(c.cfg.FFmpeg.Binary)
	if ffmpeg == "" {
		return nil
	}

	ext := filepath.Ext(outPath)
	base := strings.TrimSuffix(outPath, ext)
	tempPath := base + ".post" + ext
	args := []string{
		"-y",
		"-i", outPath,
		"-af", chatterboxPostProcessFilter(),
		tempPath,
	}

	cmd := execCommand(ffmpeg, args...)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(combined))
		if msg == "" {
			return fmt.Errorf("post-process chatterbox audio: %w", err)
		}
		return fmt.Errorf("post-process chatterbox audio: %w: %s", err, msg)
	}

	if err := os.Rename(tempPath, outPath); err != nil {
		return fmt.Errorf("replace processed chatterbox audio: %w", err)
	}
	return nil
}

func chatterboxPostProcessFilter() string {
	return fmt.Sprintf(
		"acompressor=threshold=-20dB:ratio=2.5:attack=5:release=80:makeup=2,volume=%.1fdB,alimiter=limit=0.95",
		chatterboxPostProcessGainDB,
	)
}

type daemonRequest struct {
	Text        string  `json:"text"`
	OutPath     string  `json:"out_path"`
	VoicePrompt *string `json:"voice_prompt,omitempty"`
}

type daemonResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func (c *Client) renderDaemon(text, voicePath, outPath string) error {
	conn, err := net.Dial("unix", c.cfg.TTS.Socket)
	if err != nil {
		return fmt.Errorf("dial tts daemon %s: %w", c.cfg.TTS.Socket, err)
	}
	defer conn.Close()

	req := daemonRequest{
		Text:    text,
		OutPath: outPath,
	}
	if strings.TrimSpace(voicePath) != "" {
		req.VoicePrompt = &voicePath
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send tts daemon request: %w", err)
	}

	var resp daemonResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("read tts daemon response: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("tts daemon error: %s", resp.Error)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("tts daemon response missing ok status")
	}
	return nil
}

func (c *Client) renderOneShot(text, voicePath, outPath string) error {
	pythonPath := filepath.Join(c.cfg.TTS.DataDir, "venv", "bin", "python")
	info, err := os.Stat(pythonPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.ErrNotExist
	}

	args := []string{
		filepath.Join(c.cfg.TTS.DataDir, "daemon.py"),
		"--one-shot",
		text,
		outPath,
	}
	if strings.TrimSpace(voicePath) != "" {
		args = append(args, "--voice", voicePath)
	}

	cmd := execCommand(pythonPath, args...)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(combined))
		if msg == "" {
			return fmt.Errorf("run one-shot chatterbox: %w", err)
		}
		return fmt.Errorf("run one-shot chatterbox: %w: %s", err, msg)
	}
	return nil
}

func (c *Client) renderSystem(text, outPath string) error {
	binary := strings.TrimSpace(c.cfg.TTS.FallbackBinary)
	if binary == "" {
		return fmt.Errorf("No TTS binary found. Install: claw-radio tts install  OR  apt install espeak-ng")
	}

	base := filepath.Base(binary)
	args := []string{"-w", outPath, text}
	if base == "say" {
		args = []string{"-o", outPath, text}
	}

	cmd := execCommand(binary, args...)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(combined))
		if msg == "" {
			return fmt.Errorf("run fallback tts %q: %w", base, err)
		}
		return fmt.Errorf("run fallback tts %q: %w: %s", base, err, msg)
	}
	return nil
}

func isDaemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}
