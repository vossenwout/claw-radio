package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Long:  "Use this in your radio loop after start. Each call waits for one cue, prints one agent-friendly JSON response, and exits.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPoll(cmd, pollTimeoutFlag)
	},
}

type pollCue struct {
	Event             string `json:"event"`
	Action            string `json:"action,omitempty"`
	Instruction       string `json:"instruction,omitempty"`
	UpcomingSong      string `json:"upcoming_song,omitempty"`
	SuggestedAddCount int    `json:"suggested_add_count,omitempty"`
	TS                int64  `json:"ts"`
}

func runPoll(cmd *cobra.Command, timeout time.Duration) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !pidFileRunning(pidFilePath(mpvPIDFileName)) || !pidFileRunning(pidFilePath(controllerPIDFile)) {
		event := station.AgentEvent{Event: "engine_stopped", TS: time.Now().Unix()}
		data, err := json.Marshal(toPollCue(event))
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

	cue := toPollCue(event)
	if event.Event == "timeout" {
		snapshot := buildStatusSnapshot(cfg)
		if snapshot.Engine == "running" && snapshot.Controller == "running" && snapshot.Playback != nil && snapshot.Playback.State == "buffering" {
			cue = pollCue{
				Event:       "buffering",
				Action:      "wait",
				Instruction: "Radio is buffering upcoming songs. Poll again shortly.",
				TS:          event.TS,
			}
		}
	}

	data, err := json.Marshal(cue)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func toPollCue(event station.AgentEvent) pollCue {
	cue := pollCue{Event: event.Event, TS: event.TS}

	switch event.Event {
	case "banter_needed":
		cue.Action = "speak"
		cue.Instruction = "Say 1-2 short host sentences before the upcoming song."
		if event.NextSong != nil {
			artist := strings.TrimSpace(event.NextSong.Artist)
			title := strings.TrimSpace(event.NextSong.Title)
			if artist != "" && title != "" {
				cue.UpcomingSong = artist + " - " + title
			} else {
				cue.UpcomingSong = strings.TrimSpace(title)
			}
		}
	case "queue_low":
		cue.Action = "add_songs"
		cue.Instruction = "Add more songs with `claw-radio playlist add '[...]'`."
		suggested := event.Depth - event.Count
		if suggested <= 0 {
			suggested = 3
		}
		cue.SuggestedAddCount = suggested
	case "engine_stopped":
		cue.Action = "restart"
		cue.Instruction = "Run `claw-radio start` to restart playback."
	case "timeout":
		cue.Action = "wait"
		cue.Instruction = "No new cue yet. Poll again."
	}

	return cue
}

func init() {
	pollCmd.Flags().DurationVar(&pollTimeoutFlag, "timeout", 30*time.Second, "How long to wait before returning a timeout cue")
	RootCmd.AddCommand(pollCmd)
}
