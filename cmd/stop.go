package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "End the current radio session",
	Long:  "Stop the radio and reset it for a fresh next start by clearing playlist, state, and cached audio.",
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
	runtimeChanged, err := stopRuntime(cfg)
	if err != nil {
		return err
	}

	stateChanged, err := clearStationData(cfg)
	if err != nil {
		return err
	}

	if !runtimeChanged && !stateChanged {
		fmt.Fprintln(cmd.OutOrStdout(), "radio is already stopped")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "radio stopped and reset")
	return nil
}

func clearStationData(cfg *config.Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	changed := false
	for _, path := range []string{cfg.Station.StateDir, cfg.Station.CacheDir} {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !fileExists(path) {
			continue
		}
		if err := removeAllWithRetry(path); err != nil {
			return changed, fmt.Errorf("clear path %s: %w", path, err)
		}
		changed = true
	}
	return changed, nil
}

func removeAllWithRetry(path string) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		err := removeAllFn(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		lastErr = err
		if !isRetryableRemoveAllErr(err) {
			return err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

func isRetryableRemoveAllErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EBUSY) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "directory not empty") || strings.Contains(msg, "resource busy")
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
