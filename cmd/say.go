package cmd

import (
	"encoding/json"
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
	newSayTTSClientFn = func(cfg *config.Config) sayTTSRenderer {
		return tts.NewClient(cfg)
	}
	nowUnixNanoFn = func() int64 {
		return time.Now().UnixNano()
	}
)

var sayCmd = &cobra.Command{
	Use:   "say <text>",
	Short: "Speak a host line next",
	Long:  "Use this to inject host banter between songs. If the radio is running, the line plays next. If not, it becomes your intro when you start.",
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

	banterDir := filepath.Join(cfg.TTS.DataDir, "banter")
	if err := mkdirAllFn(banterDir, 0o755); err != nil {
		return fmt.Errorf("create banter dir: %w", err)
	}
	ext := tts.OutputExtension(cfg)
	outPath := filepath.Join(banterDir, fmt.Sprintf("%d%s", nowUnixNanoFn(), ext))

	if err := newSayTTSClientFn(cfg).Render(text, "", outPath); err != nil {
		return exitCode(fmt.Errorf("tts unavailable: %w", err), 4)
	}
	if err := writeBanterSidecar(outPath, text); err != nil {
		return fmt.Errorf("save banter metadata: %w", err)
	}

	store := station.NewAgentEventStore(cfg.Station.StateDir)
	mpvClient, err := dialPlaybackClient(cfg.MPV.Socket)
	if err == nil {
		defer mpvClient.Close()
		if err := mpvClient.QueueNext(outPath); err != nil {
			return fmt.Errorf("queue banter: %w", err)
		}
		_ = store.ClearPendingBanter()
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

func writeBanterSidecar(audioPath, text string) error {
	payload := struct {
		Text string `json:"text"`
	}{Text: strings.TrimSpace(text)}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(strings.TrimSpace(audioPath)+".meta.json", data, 0o644)
}

func init() {
	RootCmd.AddCommand(sayCmd)
}
