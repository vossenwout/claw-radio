package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "End the current radio session",
	Long:  "Stop the radio and end playback for now. Your saved playlist pool stays available for the next start.",
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
	changed, err := stopRuntime(cfg)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintln(cmd.OutOrStdout(), "radio is already stopped")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "radio stopped")
	return nil
}

func stopRuntime(cfg *config.Config) (bool, error) {
	pidFiles, err := listPIDFiles()
	if err != nil {
		return false, err
	}

	mpvRunning := pidFileRunning(pidFilePath(mpvPIDFileName))
	controllerRunning := pidFileRunning(pidFilePath(controllerPIDFile))
	mpvSocketExists := cfg != nil && fileExists(strings.TrimSpace(cfg.MPV.Socket))
	ttsSocketExists := cfg != nil && fileExists(strings.TrimSpace(cfg.TTS.Socket))

	changed := mpvRunning || controllerRunning || len(pidFiles) > 0 || mpvSocketExists || ttsSocketExists
	if !changed {
		return false, nil
	}

	for _, pidFile := range pidFiles {
		pid, err := readPIDFile(pidFile)
		if err == nil && pid > 0 {
			_ = terminatePID(pid)
		}
	}

	if cfg != nil {
		_ = sendMPVQuitFn(cfg.MPV.Socket)
	}

	for _, pidFile := range pidFiles {
		_ = removeFileQuietly(pidFile)
	}

	if cfg != nil {
		_ = removeFileQuietly(cfg.MPV.Socket)
		_ = removeFileQuietly(cfg.TTS.Socket)
	}

	return true, nil
}

func fileExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func init() {
	RootCmd.AddCommand(stopCmd)
}
