package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop mpv engine and controller",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop(cmd)
	},
}

func runStop(cmd *cobra.Command) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pidFiles, err := listPIDFiles()
	if err != nil {
		return err
	}

	for _, pidFile := range pidFiles {
		pid, err := readPIDFile(pidFile)
		if err == nil && pid > 0 {
			_ = terminatePID(pid)
		}
	}

	_ = sendMPVQuitFn(cfg.MPV.Socket)

	for _, pidFile := range pidFiles {
		_ = removeFileQuietly(pidFile)
	}
	_ = removeFileQuietly(cfg.MPV.Socket)
	_ = removeFileQuietly(cfg.TTS.Socket)

	fmt.Fprintln(cmd.OutOrStdout(), "claw-radio stopped")
	return nil
}

func init() {
	RootCmd.AddCommand(stopCmd)
}
