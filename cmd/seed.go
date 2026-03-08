package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/station"
)

var (
	playlistViewJSONFlag bool
)

var playlistCmd = &cobra.Command{
	Use:   "playlist",
	Short: "Manage the radio playlist source pool",
	Long: strings.Join([]string{
		"Manage the saved song pool the radio uses for continuous playback.",
		"Use `playlist add` with a JSON string array. Each item can be a clean",
		"'Artist - Title' pair or a rough search phrase.",
	}, "\n"),
	Args: cobra.NoArgs,
}

var playlistAddCmd = &cobra.Command{
	Use:   "add <json-array>",
	Short: "Add songs to the playlist pool",
	Long: strings.Join([]string{
		"Add songs to the playlist pool using a JSON string array.",
		"Preferred format is 'Artist - Title' per item.",
		"You can also include rough query strings; the resolver will try to find",
		"a playable match when tracks are queued.",
	}, "\n"),
	Example: strings.Join([]string{
		"  claw-radio playlist add '[\"Britney Spears - Oops! I Did It Again\",\"NSYNC - Bye Bye Bye\"]'",
		"  claw-radio playlist add '[\"Daft Punk - One More Time\",\"Outkast - Hey Ya!\",\"SZA - Saturn\"]'",
		"  claw-radio playlist add '[\"best 2000s pop song with female vocals\",\"Kendrick Lamar Alright clean version\"]'",
		"  claw-radio playlist add '[\"The Weeknd - Blinding Lights\",\"90s eurodance club anthem\"]'",
	}, "\n"),
	Args: playlistAddArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlaylistAdd(cmd, args[0])
	},
}

var playlistResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear all songs from the playlist pool",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlaylistReset(cmd)
	},
}

var playlistViewCmd = &cobra.Command{
	Use:   "view",
	Short: "View songs currently in the playlist pool",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlaylistView(cmd, playlistViewJSONFlag)
	},
}

func playlistAddArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return nil
	}
	_ = cmd.Help()
	return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
}

func runPlaylistAdd(cmd *cobra.Command, raw string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	seeds, err := parsePlaylistSongs(raw)
	if err != nil {
		return exitCode(err, 1)
	}

	st, err := station.Load(cfg.Station.StateDir)
	if err != nil {
		return fmt.Errorf("load station state: %w", err)
	}

	before := len(st.Seeds)
	st.AppendSeeds(seeds)
	added := len(st.Seeds) - before
	if err := st.Save(); err != nil {
		return fmt.Errorf("save station state: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added %d songs (total: %d)\n", added, len(st.Seeds))
	return nil
}

func runPlaylistReset(cmd *cobra.Command) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := station.Load(cfg.Station.StateDir)
	if err != nil {
		return fmt.Errorf("load station state: %w", err)
	}

	st.SetSeeds([]string{}, "")
	if err := st.Save(); err != nil {
		return fmt.Errorf("save station state: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Playlist reset")
	return nil
}

func runPlaylistView(cmd *cobra.Command, asJSON bool) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := station.Load(cfg.Station.StateDir)
	if err != nil {
		return fmt.Errorf("load station state: %w", err)
	}

	if asJSON {
		data, err := json.Marshal(st.Seeds)
		if err != nil {
			return fmt.Errorf("marshal playlist: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	if len(st.Seeds) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Playlist is empty")
		return nil
	}

	playlistSnapshot := station.BuildPlaylistSnapshot(cfg, st.Seeds, "", nil)
	showStatuses := false
	if pidFileRunning(pidFilePath(mpvPIDFileName)) {
		client, err := dialStatusMPVClientFn(cfg.MPV.Socket)
		if err == nil {
			defer client.Close()
			currentPath, _ := readStringProperty(client, "path")
			if overview, ok := readPlaylistOverview(cfg, client); ok {
				playlistSnapshot = station.BuildPlaylistSnapshot(cfg, st.Seeds, currentPath, overview.RemainingPaths)
				showStatuses = true
			}
		}
	}

	rows := st.Seeds
	if showStatuses {
		rows = make([]string, 0, len(playlistSnapshot.Songs))
		for _, song := range playlistSnapshot.Songs {
			rows = append(rows, fmt.Sprintf("%s [%s]", song.Seed, song.Status))
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Playlist is empty")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Playlist (%d songs):\n", len(rows))
	for i, song := range rows {
		fmt.Fprintf(cmd.OutOrStdout(), "%d. %s\n", i+1, song)
	}
	return nil
}

func parsePlaylistSongs(raw string) ([]string, error) {
	var songs []string
	if err := json.Unmarshal([]byte(raw), &songs); err != nil {
		return nil, fmt.Errorf("parse playlist json: %w", err)
	}
	return songs, nil
}

func init() {
	playlistViewCmd.Flags().BoolVar(&playlistViewJSONFlag, "json", false, "Output songs as JSON array")
	playlistCmd.AddCommand(playlistAddCmd)
	playlistCmd.AddCommand(playlistViewCmd)
	playlistCmd.AddCommand(playlistResetCmd)
	RootCmd.AddCommand(playlistCmd)
}
