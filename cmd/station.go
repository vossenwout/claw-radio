package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/provider"
	"github.com/vossenwout/claw-radio/internal/station"
)

var stationCmd = &cobra.Command{
	Use:    "station",
	Hidden: true,
}

var stationDaemonCmd = &cobra.Command{
	Use:    "daemon",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if err := os.MkdirAll(cfg.Station.StateDir, 0o755); err != nil {
			return fmt.Errorf("create station state dir: %w", err)
		}

		logPath := filepath.Join(cfg.Station.StateDir, "controller.log")
		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open controller log: %w", err)
		}
		defer logFile.Close()

		prov := provider.NewYtDlpProvider(cfg.YtDlp.Binary)
		return station.Run(cfg, prov, logFile)
	},
}

func init() {
	stationCmd.AddCommand(stationDaemonCmd)
	RootCmd.AddCommand(stationCmd)
}
