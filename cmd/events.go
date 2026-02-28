package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

const eventsDefaultQueueDepth = 5

type eventsMPVClient interface {
	Close() error
	Events() <-chan map[string]interface{}
	Get(prop string) (json.RawMessage, error)
	PlaylistCount() (int, error)
}

var (
	eventsJSONFlag bool

	dialEventsMPVClientFn = func(socketPath string) (eventsMPVClient, error) {
		return mpv.Dial(socketPath)
	}
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Stream playback events",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEvents(cmd, eventsJSONFlag)
	},
}

func runEvents(cmd *cobra.Command, asJSON bool) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := dialEventsMPVClient(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	emitter := newEventsEmitter(cmd.OutOrStdout(), asJSON)

	lastTitle := ""
	for event := range client.Events() {
		name, _ := event["event"].(string)
		switch name {
		case "file-loaded":
			title := lastTitle
			if raw, err := client.Get("media-title"); err == nil {
				if parsed := parseStringRaw(raw); parsed != "" {
					title = parsed
				}
			}

			duration := 0.0
			if raw, err := client.Get("duration"); err == nil {
				duration = parseFloatRaw(raw)
			}

			if title != "" {
				lastTitle = title
			}

			if err := emitter.emit(map[string]interface{}{
				"event":    "track_started",
				"title":    title,
				"duration": duration,
				"ts":       time.Now().Unix(),
			}); err != nil {
				return fmt.Errorf("emit track_started: %w", err)
			}
		case "end-file":
			if err := emitter.emit(map[string]interface{}{
				"event": "track_ended",
				"title": lastTitle,
				"ts":    time.Now().Unix(),
			}); err != nil {
				return fmt.Errorf("emit track_ended: %w", err)
			}

			count, err := client.PlaylistCount()
			if err != nil {
				continue
			}
			if count <= 2 {
				depth := cfg.Station.QueueDepth
				if depth <= 0 {
					depth = eventsDefaultQueueDepth
				}
				if err := emitter.emit(map[string]interface{}{
					"event": "queue_low",
					"count": count,
					"depth": depth,
					"ts":    time.Now().Unix(),
				}); err != nil {
					return fmt.Errorf("emit queue_low: %w", err)
				}
			}
		}
	}

	if err := emitter.emit(map[string]interface{}{
		"event": "engine_stopped",
		"ts":    time.Now().Unix(),
	}); err != nil {
		return fmt.Errorf("emit engine_stopped: %w", err)
	}

	return nil
}

func dialEventsMPVClient(socketPath string) (eventsMPVClient, error) {
	client, err := dialEventsMPVClientFn(socketPath)
	if err == nil {
		return client, nil
	}
	return nil, exitCode(errors.New(mpvNotRunningMessage), 5)
}

type eventsEmitter struct {
	out    *bufio.Writer
	asJSON bool
}

func newEventsEmitter(w io.Writer, asJSON bool) *eventsEmitter {
	return &eventsEmitter{
		out:    bufio.NewWriter(w),
		asJSON: asJSON,
	}
}

func (e *eventsEmitter) emit(payload map[string]interface{}) error {
	if e.asJSON {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := e.out.Write(data); err != nil {
			return err
		}
		if err := e.out.WriteByte('\n'); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(e.out, formatHumanEvent(payload)); err != nil {
			return err
		}
	}
	return e.out.Flush()
}

func formatHumanEvent(event map[string]interface{}) string {
	name, _ := event["event"].(string)
	switch name {
	case "track_started":
		title, _ := event["title"].(string)
		duration := asFloat(event["duration"])
		return fmt.Sprintf("track_started: %s (%s)", title, formatDuration(duration))
	case "track_ended":
		title, _ := event["title"].(string)
		return fmt.Sprintf("track_ended: %s", title)
	case "queue_low":
		count := asInt(event["count"])
		depth := asInt(event["depth"])
		return fmt.Sprintf("queue_low: count=%d depth=%d", count, depth)
	case "engine_stopped":
		return "engine_stopped"
	default:
		return name
	}
}

func parseStringRaw(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func parseFloatRaw(raw json.RawMessage) float64 {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f
	}

	var i int
	if err := json.Unmarshal(raw, &i); err == nil {
		return float64(i)
	}

	return 0
}

func asFloat(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func asInt(v interface{}) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func formatDuration(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}

	total := int(seconds + 0.5)
	min := total / 60
	sec := total % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}

func init() {
	eventsCmd.Flags().BoolVar(&eventsJSONFlag, "json", false, "Output newline-delimited JSON")
	RootCmd.AddCommand(eventsCmd)
}
