package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

const mpvNotRunningMessage = "radio is not running. Start with: claw-radio start"

type playbackClient interface {
	Close() error
	Command(args ...interface{}) error
	LoadFile(path, mode string) error
	InsertNext(path string) error
}

var dialPlaybackClientFn = func(socketPath string) (playbackClient, error) {
	return mpv.Dial(socketPath)
}

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Skip to the next track",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNext()
	},
}

func runNext() error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Command("playlist-next"); err != nil {
		return fmt.Errorf("skip track: %w", err)
	}
	return nil
}

func dialPlaybackClient(socketPath string) (playbackClient, error) {
	client, err := dialPlaybackClientFn(socketPath)
	if err == nil {
		return client, nil
	}
	return nil, exitCode(errors.New(mpvNotRunningMessage), 5)
}

func init() {
	RootCmd.AddCommand(nextCmd)
}
