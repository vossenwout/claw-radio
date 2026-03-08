package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
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
	Prompt            string `json:"prompt,omitempty"`
	Command           string `json:"command,omitempty"`
	CommandTemplate   string `json:"command_template,omitempty"`
	UpcomingSong      string `json:"upcoming_song,omitempty"`
	SuggestedAddCount int    `json:"suggested_add_count,omitempty"`
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
	event, err := nextRelevantPollEvent(cfg, store, timeout)
	if err != nil {
		return fmt.Errorf("poll event: %w", err)
	}

	cue := toPollCue(event)
	if event.Event == "timeout" {
		snapshot := buildStatusSnapshot(cfg)
		if snapshot.Engine == "running" && snapshot.Controller == "running" && snapshot.Playback != nil && snapshot.Playback.State == "buffering" {
			cue = pollCue{
				Event:  "buffering",
				Prompt: "Radio is buffering upcoming songs. Poll again shortly.",
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
	cue := pollCue{Event: event.Event}

	switch event.Event {
	case "banter_needed":
		cue.Prompt = defaultPollPrompt(event.Prompt, "Say 1-2 short host sentences before the upcoming song.")
		cue.CommandTemplate = `claw-radio say "<banter>"`
		if event.NextSong != nil {
			artist := strings.TrimSpace(event.NextSong.Artist)
			title := strings.TrimSpace(event.NextSong.Title)
			if artist != "" && title != "" {
				cue.UpcomingSong = artist + " - " + title
			} else if strings.TrimSpace(event.NextSong.Seed) != "" {
				cue.UpcomingSong = strings.TrimSpace(event.NextSong.Seed)
			} else {
				cue.UpcomingSong = strings.TrimSpace(title)
			}
		}
	case "queue_low":
		cue.Prompt = defaultPollPrompt(event.Prompt, "Add more songs to keep the queue healthy.")
		cue.CommandTemplate = `claw-radio playlist add '["Artist - Title", ...]'`
		suggested := event.Depth - event.Count
		if suggested <= 0 {
			suggested = 3
		}
		cue.SuggestedAddCount = suggested
	case "engine_stopped":
		cue.Prompt = defaultPollPrompt(event.Prompt, "Restart playback.")
		cue.Command = "claw-radio start"
	case "timeout":
		cue.Prompt = defaultPollPrompt(event.Prompt, "No new cue yet. Poll again.")
	}

	return cue
}

func defaultPollPrompt(raw, fallback string) string {
	prompt := strings.TrimSpace(raw)
	if prompt != "" {
		return prompt
	}
	return fallback
}

func nextRelevantPollEvent(cfg *config.Config, store *station.AgentEventStore, timeout time.Duration) (station.AgentEvent, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return station.AgentEvent{Event: "timeout", TS: time.Now().Unix()}, nil
		}

		event, err := store.Next(remaining)
		if err != nil {
			return station.AgentEvent{}, err
		}
		if event.Event == "timeout" {
			return event, nil
		}
		if pollEventStillRelevant(cfg, store, event) {
			return event, nil
		}
	}
}

func pollEventStillRelevant(cfg *config.Config, store *station.AgentEventStore, event station.AgentEvent) bool {
	switch event.Event {
	case "banter_needed":
		return banterEventStillRelevant(cfg, store, event)
	case "queue_low":
		return queueLowEventStillRelevant(cfg)
	default:
		return true
	}
}

func banterEventStillRelevant(cfg *config.Config, store *station.AgentEventStore, event station.AgentEvent) bool {
	if strings.TrimSpace(event.EventID) == "" {
		return true
	}
	pending, err := store.LoadPendingBanter()
	if err != nil {
		return true
	}
	if pending == nil || strings.TrimSpace(pending.EventID) != strings.TrimSpace(event.EventID) {
		return false
	}
	if cfg == nil || event.NextSong == nil {
		return true
	}

	currentPath, _, ok := loadPollRuntimeState(cfg)
	if !ok {
		return true
	}
	st, err := station.Load(cfg.Station.StateDir)
	if err != nil || st == nil {
		return true
	}
	nextSong, exists := station.NextAgentSong(st.Seeds, currentPath)
	if !exists {
		return false
	}
	return station.SameAgentSong(*event.NextSong, nextSong)
}

func queueLowEventStillRelevant(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	currentPath, remainingPaths, ok := loadPollRuntimeState(cfg)
	if !ok {
		currentPath = ""
		remainingPaths = nil
	}
	st, err := station.Load(cfg.Station.StateDir)
	if err != nil || st == nil {
		return true
	}
	snapshot := station.BuildPlaylistSnapshot(cfg, st.Seeds, currentPath, remainingPaths)
	return snapshot.Ready+snapshot.Preparing <= station.QueueLowThreshold
}

func loadPollRuntimeState(cfg *config.Config) (string, []string, bool) {
	if cfg == nil {
		return "", nil, false
	}
	client, err := dialStatusMPVClientFn(cfg.MPV.Socket)
	if err != nil {
		return "", nil, false
	}
	defer client.Close()

	currentPath, _ := readStringProperty(client, "path")
	overview, ok := readPlaylistOverview(cfg, client)
	if !ok {
		return currentPath, nil, false
	}
	return currentPath, overview.RemainingPaths, true
}

func init() {
	pollCmd.Flags().DurationVar(&pollTimeoutFlag, "timeout", 30*time.Second, "How long to wait before returning a timeout cue")
	RootCmd.AddCommand(pollCmd)
}
