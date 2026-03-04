package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Start fresh from a clean slate",
	Long:  "Stop the radio and clear your saved playlist pool, station state, and cache so you can start over from scratch.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runReset(cmd)
	},
}

func runReset(cmd *cobra.Command) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if _, err := stopRuntime(cfg); err != nil {
		return err
	}

	for _, path := range []string{cfg.Station.StateDir, cfg.Station.CacheDir} {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if err := removeAllFn(path); err != nil {
			return fmt.Errorf("clear reset path %s: %w", path, err)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), "radio reset")
	return nil
}

func init() {
	RootCmd.AddCommand(resetCmd)
}
