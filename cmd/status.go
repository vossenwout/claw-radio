package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

type statusMPVClient interface {
	Close() error
	Get(prop string) (json.RawMessage, error)
	PlaylistCount() (int, error)
}

type statusSnapshot struct {
	Engine     string          `json:"engine"`
	Station    *statusStation  `json:"station,omitempty"`
	Playback   *statusPlayback `json:"playback,omitempty"`
	Queue      statusQueue     `json:"queue"`
	Controller string          `json:"controller"`
	TTS        string          `json:"tts"`
}

type statusStation struct {
	Label string `json:"label"`
	Seeds int    `json:"seeds"`
}

type statusPlayback struct {
	State    string  `json:"state"`
	Title    string  `json:"title"`
	TimePos  float64 `json:"time_pos"`
	Duration float64 `json:"duration"`
	Volume   float64 `json:"volume"`
}

type statusQueue struct {
	Upcoming int `json:"upcoming"`
}

type statusStationFile struct {
	Label string   `json:"label"`
	Seeds []string `json:"seeds"`
}

var (
	statusJSONFlag bool

	dialStatusMPVClientFn = func(socketPath string) (statusMPVClient, error) {
		return mpv.Dial(socketPath)
	}
	statusReadFileFn       = os.ReadFile
	statusProcessRunningFn = isProcessRunning
	statusDialTimeoutFn    = net.DialTimeout
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check what the radio is doing right now",
	Long:  "Show whether the radio is running, what is currently playing, and how full the queue is.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(cmd, statusJSONFlag)
	},
}

func runStatus(cmd *cobra.Command, asJSON bool) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	snapshot := buildStatusSnapshot(cfg)

	if asJSON {
		data, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal status json: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	renderHumanStatus(cmd.OutOrStdout(), snapshot)
	return nil
}

func buildStatusSnapshot(cfg *config.Config) statusSnapshot {
	snapshot := statusSnapshot{
		Engine:     "stopped",
		Queue:      statusQueue{Upcoming: 0},
		Controller: "stopped",
		TTS:        detectTTSStatus(cfg),
	}

	if cfg == nil {
		return snapshot
	}

	if st, ok := readStationSummary(cfg.Station.StateDir); ok {
		snapshot.Station = st
	}

	if pidFileRunning(pidFilePath(mpvPIDFileName)) {
		snapshot.Engine = "running"
	}
	if pidFileRunning(pidFilePath(controllerPIDFile)) {
		snapshot.Controller = "running"
	}

	if snapshot.Engine != "running" {
		return snapshot
	}

	client, err := dialStatusMPVClientFn(cfg.MPV.Socket)
	if err != nil {
		return snapshot
	}
	defer client.Close()

	snapshot.Playback = readPlaybackStatus(client)
	if upcoming, ok := readUpcomingFromPlaylist(client); ok {
		snapshot.Queue.Upcoming = upcoming
	} else if count, err := client.PlaylistCount(); err == nil {
		upcoming := count
		if snapshot.Playback != nil && (snapshot.Playback.State == "playing" || snapshot.Playback.State == "paused" || snapshot.Playback.State == "buffering") && upcoming > 0 {
			upcoming--
		}
		snapshot.Queue.Upcoming = upcoming
	}
	if snapshot.Playback != nil && snapshot.Playback.State == "idle" && (snapshot.Queue.Upcoming > 0 || (snapshot.Station != nil && snapshot.Station.Seeds > 0)) {
		snapshot.Playback.State = "buffering"
	}

	return snapshot
}

func pidFileRunning(path string) bool {
	pid, err := readPIDFile(path)
	if err != nil || pid <= 0 {
		return false
	}
	return statusProcessRunningFn(pid)
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if isNoSuchProcessError(err) {
		return false
	}
	return true
}

func readStationSummary(stateDir string) (*statusStation, bool) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, false
	}

	data, err := statusReadFileFn(filepath.Join(stateDir, "station.json"))
	if err != nil {
		return nil, false
	}

	var station statusStationFile
	if err := json.Unmarshal(data, &station); err != nil {
		return nil, false
	}

	return &statusStation{
		Label: station.Label,
		Seeds: len(station.Seeds),
	}, true
}

func readPlaybackStatus(client statusMPVClient) *statusPlayback {
	playback := &statusPlayback{
		State: "playing",
	}

	if paused, ok := readBoolProperty(client, "pause"); ok && paused {
		playback.State = "paused"
	}

	playback.Title = resolvePlaybackTitle(client)
	if timePos, ok := readFloatProperty(client, "time-pos"); ok {
		playback.TimePos = timePos
	}
	if duration, ok := readFloatProperty(client, "duration"); ok {
		playback.Duration = duration
	}
	if volume, ok := readFloatProperty(client, "volume"); ok {
		playback.Volume = volume
	}

	if playback.Title == "" && playback.State == "playing" {
		playback.State = "idle"
	}

	return playback
}

func readStringProperty(client statusMPVClient, prop string) (string, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func readBoolProperty(client statusMPVClient, prop string) (bool, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func readFloatProperty(client statusMPVClient, prop string) (float64, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return 0, false
	}

	var floatValue float64
	if err := json.Unmarshal(raw, &floatValue); err == nil {
		return floatValue, true
	}

	var intValue int
	if err := json.Unmarshal(raw, &intValue); err == nil {
		return float64(intValue), true
	}

	return 0, false
}

func readUpcomingFromPlaylist(client statusMPVClient) (int, bool) {
	raw, err := client.Get("playlist")
	if err != nil {
		return 0, false
	}
	var items []struct {
		Current bool `json:"current"`
		Playing bool `json:"playing"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, false
	}
	if len(items) == 0 {
		return 0, true
	}
	currentIndex := -1
	for i, item := range items {
		if item.Current || item.Playing {
			currentIndex = i
			break
		}
	}
	if currentIndex < 0 {
		return 0, true
	}
	upcoming := len(items) - currentIndex - 1
	if upcoming < 0 {
		upcoming = 0
	}
	return upcoming, true
}

func detectTTSStatus(cfg *config.Config) string {
	if cfg == nil {
		return "unavailable"
	}
	if ttsSocketResponsive(cfg.TTS.Socket) {
		return "warm"
	}
	if strings.TrimSpace(cfg.TTS.FallbackBinary) != "" {
		return "system"
	}
	return "unavailable"
}

func ttsSocketResponsive(socketPath string) bool {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return false
	}

	conn, err := statusDialTimeoutFn("unix", socketPath, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func renderHumanStatus(w io.Writer, snapshot statusSnapshot) {
	fmt.Fprintf(w, "engine: %s\n", snapshot.Engine)
	if snapshot.Station != nil {
		fmt.Fprintf(w, "station: label=%q seeds=%d\n", snapshot.Station.Label, snapshot.Station.Seeds)
	} else {
		fmt.Fprintln(w, "station: unavailable")
	}

	if snapshot.Playback != nil {
		fmt.Fprintf(
			w,
			"playback: state=%s title=%q time_pos=%.1f duration=%.1f volume=%.0f\n",
			snapshot.Playback.State,
			snapshot.Playback.Title,
			snapshot.Playback.TimePos,
			snapshot.Playback.Duration,
			snapshot.Playback.Volume,
		)
	} else {
		fmt.Fprintln(w, "playback: unavailable")
	}

	fmt.Fprintf(w, "upcoming songs: %d\n", snapshot.Queue.Upcoming)
	fmt.Fprintf(w, "controller: %s\n", snapshot.Controller)
	fmt.Fprintf(w, "tts: %s\n", snapshot.TTS)
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSONFlag, "json", false, "Return status as JSON")
	RootCmd.AddCommand(statusCmd)
}
