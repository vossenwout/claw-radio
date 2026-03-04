package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/station"
)

var (
	pollJSONFlag    bool
	pollTimeoutFlag time.Duration
)

var pollCmd = &cobra.Command{
	Use:   "poll",
	Short: "Wait for one actionable controller event",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPoll(cmd, pollJSONFlag, pollTimeoutFlag)
	},
}

func runPoll(cmd *cobra.Command, asJSON bool, timeout time.Duration) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !pidFileRunning(pidFilePath(mpvPIDFileName)) || !pidFileRunning(pidFilePath(controllerPIDFile)) {
		event := station.AgentEvent{Event: "engine_stopped", TS: time.Now().Unix()}
		if asJSON {
			data, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("marshal event: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), event.Event)
		return err
	}

	store := station.NewAgentEventStore(cfg.Station.StateDir)
	event, err := store.Next(timeout)
	if err != nil {
		return fmt.Errorf("poll event: %w", err)
	}

	if asJSON {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	if event.Event == "banter_needed" && event.NextSong != nil {
		display := event.NextSong.Title
		if event.NextSong.Artist != "" {
			display = event.NextSong.Artist + " - " + event.NextSong.Title
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "banter_needed: %s\n", display)
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), event.Event)
	return err
}

func init() {
	pollCmd.Flags().BoolVar(&pollJSONFlag, "json", true, "Output JSON event payload")
	pollCmd.Flags().DurationVar(&pollTimeoutFlag, "timeout", 30*time.Second, "Maximum time to wait for an event")
	RootCmd.AddCommand(pollCmd)
}
