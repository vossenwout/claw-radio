package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/mpv"
	"github.com/vossenwout/claw-radio/internal/provider"
)

const mpvNotRunningMessage = "mpv not running. Start with: claw-radio start"

type playbackClient interface {
	Close() error
	Command(args ...interface{}) error
	Set(prop string, value interface{}) error
	LoadFile(path, mode string) error
	InsertNext(path string) error
}

type playbackResolver interface {
	Resolve(seed, cacheDir string) (audioPath string, err error)
}

var (
	dialPlaybackClientFn = func(socketPath string) (playbackClient, error) {
		return mpv.Dial(socketPath)
	}
	newPlaybackResolverFn = func(binary string) playbackResolver {
		return provider.NewYtDlpProvider(binary)
	}
)

var playCmd = &cobra.Command{
	Use:   "play <query|url>",
	Short: "Insert a song next and skip to it immediately",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlay(args[0])
	},
}

var queueCmd = &cobra.Command{
	Use:   "queue <query|url>",
	Short: "Append a song to the end of the queue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQueue(args[0])
	},
}

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause playback",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPause()
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume playback",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runResume()
	},
}

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Skip to the next track",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNext()
	},
}

func runPlay(seed string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	path, err := newPlaybackResolverFn(cfg.YtDlp.Binary).Resolve(seed, cfg.Station.CacheDir)
	if err != nil {
		return fmt.Errorf("resolve track: %w", err)
	}

	if err := client.InsertNext(path); err != nil {
		return fmt.Errorf("insert next: %w", err)
	}
	if err := client.Command("playlist-next"); err != nil {
		return fmt.Errorf("skip to inserted track: %w", err)
	}
	return nil
}

func runQueue(seed string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	path, err := newPlaybackResolverFn(cfg.YtDlp.Binary).Resolve(seed, cfg.Station.CacheDir)
	if err != nil {
		return fmt.Errorf("resolve track: %w", err)
	}

	if err := client.LoadFile(path, "append"); err != nil {
		return fmt.Errorf("append track: %w", err)
	}
	return nil
}

func runPause() error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Set("pause", true); err != nil {
		return fmt.Errorf("pause playback: %w", err)
	}
	return nil
}

func runResume() error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialPlaybackClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Set("pause", false); err != nil {
		return fmt.Errorf("resume playback: %w", err)
	}
	return nil
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
	RootCmd.AddCommand(playCmd)
	RootCmd.AddCommand(queueCmd)
	RootCmd.AddCommand(pauseCmd)
	RootCmd.AddCommand(resumeCmd)
	RootCmd.AddCommand(nextCmd)
}
