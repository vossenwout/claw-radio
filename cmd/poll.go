package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/station"
)

var (
	pollTimeoutFlag time.Duration
)

var pollCmd = &cobra.Command{
	Use:   "poll",
	Short: "Get the next host cue",
	Long:  "Use this in your radio loop after start. Each call waits for one cue (like banter_needed, queue_low, engine_stopped, or timeout), prints JSON, and exits.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPoll(cmd, pollTimeoutFlag)
	},
}

func runPoll(cmd *cobra.Command, timeout time.Duration) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !pidFileRunning(pidFilePath(mpvPIDFileName)) || !pidFileRunning(pidFilePath(controllerPIDFile)) {
		event := station.AgentEvent{Event: "engine_stopped", TS: time.Now().Unix()}
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	store := station.NewAgentEventStore(cfg.Station.StateDir)
	event, err := store.Next(timeout)
	if err != nil {
		return fmt.Errorf("poll event: %w", err)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func init() {
	pollCmd.Flags().DurationVar(&pollTimeoutFlag, "timeout", 30*time.Second, "How long to wait before returning a timeout cue")
	RootCmd.AddCommand(pollCmd)
}
