package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/tts"
)

type sayTTSRenderer interface {
	Render(text, voicePath, outPath string) error
}

var (
	sayVoiceFlag string

	newSayTTSClientFn = func(cfg *config.Config) sayTTSRenderer {
		return tts.NewClient(cfg)
	}
	nowUnixNanoFn = func() int64 {
		return time.Now().UnixNano()
	}
)

var sayCmd = &cobra.Command{
	Use:   "say <text>",
	Short: "Render banter text and queue it after the current track",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSay(cmd, args[0])
	},
}

func runSay(cmd *cobra.Command, text string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mpvClient, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer mpvClient.Close()

	voicePath := resolveVoicePath(sayVoiceFlag, cfg)

	banterDir := filepath.Join(cfg.TTS.DataDir, "banter")
	if err := mkdirAllFn(banterDir, 0o755); err != nil {
		return fmt.Errorf("create banter dir: %w", err)
	}
	outPath := filepath.Join(banterDir, fmt.Sprintf("%d.wav", nowUnixNanoFn()))

	if err := newSayTTSClientFn(cfg).Render(text, voicePath, outPath); err != nil {
		return exitCode(fmt.Errorf("tts unavailable: %w", err), 4)
	}

	if err := mpvClient.InsertNext(outPath); err != nil {
		return fmt.Errorf("queue banter: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "queued banter")
	return nil
}

func resolveVoicePath(name string, cfg *config.Config) string {
	if cfg == nil {
		return ""
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}

	if p, ok := cfg.TTS.Voices[name]; ok && strings.TrimSpace(p) != "" {
		return p
	}

	candidate := filepath.Join(cfg.TTS.DataDir, "voices", name+".wav")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}

	if info, err := os.Stat(name); err == nil && !info.IsDir() {
		return name
	}

	return ""
}

func init() {
	sayCmd.Flags().StringVar(&sayVoiceFlag, "voice", "", "Voice profile name or path to .wav")
	RootCmd.AddCommand(sayCmd)
}
