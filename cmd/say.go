package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/station"
	"github.com/vossenwout/claw-radio/internal/tts"
)

type sayTTSRenderer interface {
	Render(text, voicePath, outPath string) error
}

var (
	sayVoiceFlag string
	sayForFlag   string

	newSayTTSClientFn = func(cfg *config.Config) sayTTSRenderer {
		return tts.NewClient(cfg)
	}
	nowUnixNanoFn = func() int64 {
		return time.Now().UnixNano()
	}
)

var sayCmd = &cobra.Command{
	Use:   "say <text>",
	Short: "Render banter text and queue it now or for next start",
	Long:  "Render banter via TTS. If mpv is running, queue after current track; if not, store as intro for next start. Use --for to fulfill a pending banter event.",
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

	voicePath := resolveVoicePath(sayVoiceFlag, cfg)

	banterDir := filepath.Join(cfg.TTS.DataDir, "banter")
	if err := mkdirAllFn(banterDir, 0o755); err != nil {
		return fmt.Errorf("create banter dir: %w", err)
	}
	ext := ".wav"
	if strings.EqualFold(filepath.Base(strings.TrimSpace(cfg.TTS.FallbackBinary)), "say") {
		ext = ".aiff"
	}
	outPath := filepath.Join(banterDir, fmt.Sprintf("%d%s", nowUnixNanoFn(), ext))

	if err := newSayTTSClientFn(cfg).Render(text, voicePath, outPath); err != nil {
		return exitCode(fmt.Errorf("tts unavailable: %w", err), 4)
	}

	store := station.NewAgentEventStore(cfg.Station.StateDir)
	requestedEventID := strings.TrimSpace(sayForFlag)
	if requestedEventID != "" {
		pending, err := store.LoadPendingBanter()
		if err != nil {
			return fmt.Errorf("load pending banter: %w", err)
		}
		if pending == nil {
			return exitCode(fmt.Errorf("no pending banter event found for --for %q", requestedEventID), 1)
		}
		if pending.EventID != requestedEventID {
			return exitCode(fmt.Errorf("pending banter event is %q (requested %q)", pending.EventID, requestedEventID), 1)
		}
		if pending.Fulfilled {
			fmt.Fprintln(cmd.OutOrStdout(), "banter already queued")
			return nil
		}

		mpvClient, err := dialPlaybackClient(cfg.MPV.Socket)
		if err != nil {
			return err
		}
		defer mpvClient.Close()

		if err := mpvClient.InsertNext(outPath); err != nil {
			return fmt.Errorf("queue banter: %w", err)
		}

		pending.Fulfilled = true
		pending.FulfilledAt = time.Now().Unix()
		if err := store.SavePendingBanter(*pending); err != nil {
			return fmt.Errorf("save pending banter: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "queued banter")
		return nil
	}

	mpvClient, err := dialPlaybackClient(cfg.MPV.Socket)
	if err == nil {
		defer mpvClient.Close()
		if err := mpvClient.InsertNext(outPath); err != nil {
			return fmt.Errorf("queue banter: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "queued banter")
		return nil
	}

	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 5 {
		return err
	}

	if err := store.SavePendingIntro(outPath); err != nil {
		return fmt.Errorf("queue intro banter for next start: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "queued intro banter for next start")
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
	sayCmd.Flags().StringVar(&sayForFlag, "for", "", "Fulfill a pending banter event id")
	RootCmd.AddCommand(sayCmd)
}
