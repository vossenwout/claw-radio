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
	"github.com/vossenwout/claw-radio/internal/station"
)

type statusMPVClient interface {
	Close() error
	Get(prop string) (json.RawMessage, error)
	PlaylistCount() (int, error)
}

type statusSnapshot struct {
	Engine     string          `json:"engine"`
	Playback   *statusPlayback `json:"playback,omitempty"`
	Queue      statusQueue     `json:"queue"`
	Banter     string          `json:"banter,omitempty"`
	Warning    string          `json:"warning,omitempty"`
	Controller string          `json:"controller"`
	TTS        string          `json:"tts"`
}

type statusPlayback struct {
	State    string  `json:"state"`
	Title    string  `json:"title"`
	TimePos  float64 `json:"time_pos"`
	Duration float64 `json:"duration"`
	Volume   float64 `json:"volume"`
}

type statusQueue struct {
	Upcoming  int `json:"upcoming"`
	Preparing int `json:"preparing"`
}

type statusPlaylistEntry struct {
	Filename string `json:"filename"`
	Current  bool   `json:"current"`
	Playing  bool   `json:"playing"`
}

type statusPlaylistOverview struct {
	RemainingPaths []string
	UpcomingSongs  int
	Banter         string
}

const statusBanterPreviewLimit = 80

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

	queueSeeds := loadStatusSeeds(cfg.Station.StateDir)
	snapshot.Queue.Preparing = len(queueSeeds)
	snapshot.Banter = readPendingIntroBanter(cfg)

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
	currentPath, _ := readStringProperty(client, "path")
	if overview, ok := readPlaylistOverview(cfg, client); ok {
		snapshot.Banter = overview.Banter
		queueSnapshot := station.BuildPlaylistSnapshot(cfg, queueSeeds, currentPath, overview.RemainingPaths)
		if len(queueSeeds) > 0 || len(queueSnapshot.Songs) > 0 {
			snapshot.Queue.Upcoming = queueSnapshot.Ready
			snapshot.Queue.Preparing = queueSnapshot.Preparing
		} else {
			snapshot.Queue.Upcoming = overview.UpcomingSongs
		}
	} else if count, err := client.PlaylistCount(); err == nil {
		upcoming := count
		if snapshot.Playback != nil && (snapshot.Playback.State == "playing" || snapshot.Playback.State == "paused" || snapshot.Playback.State == "buffering") && upcoming > 0 {
			upcoming--
		}
		snapshot.Queue.Upcoming = upcoming
	}
	if snapshot.Playback != nil && snapshot.Playback.State == "idle" && (snapshot.Queue.Upcoming > 0 || snapshot.Queue.Preparing > 0) {
		snapshot.Playback.State = "buffering"
	}
	if snapshot.Playback != nil && snapshot.Playback.State == "idle" && snapshot.Queue.Upcoming == 0 && snapshot.Queue.Preparing == 0 {
		snapshot.Banter = ""
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

func readPreparingCount(stateDir string) (int, bool) {
	if strings.TrimSpace(stateDir) == "" {
		return 0, false
	}
	st, err := station.Load(stateDir)
	if err != nil || st == nil {
		return 0, false
	}
	return len(st.Seeds), true
}

func loadStatusSeeds(stateDir string) []string {
	st, err := station.Load(stateDir)
	if err != nil || st == nil {
		return nil
	}
	return append([]string(nil), st.Seeds...)
}

func readPendingIntroBanter(cfg *config.Config) string {
	if cfg == nil || strings.TrimSpace(cfg.Station.StateDir) == "" {
		return ""
	}
	pending, err := station.NewAgentEventStore(cfg.Station.StateDir).LoadPendingIntro()
	if err != nil || pending == nil {
		return ""
	}
	audioPath := strings.TrimSpace(pending.AudioPath)
	if audioPath == "" {
		return ""
	}
	return readStatusBanterText(audioPath)
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

func readPlaylistOverview(cfg *config.Config, client statusMPVClient) (statusPlaylistOverview, bool) {
	raw, err := client.Get("playlist")
	if err != nil {
		return statusPlaylistOverview{}, false
	}
	var items []statusPlaylistEntry
	if err := json.Unmarshal(raw, &items); err != nil {
		return statusPlaylistOverview{}, false
	}
	overview := statusPlaylistOverview{}
	if len(items) == 0 {
		return overview, true
	}
	currentIndex := -1
	for i, item := range items {
		if item.Current || item.Playing {
			currentIndex = i
			break
		}
	}
	pathStart := 0
	if currentIndex >= 0 {
		pathStart = currentIndex
	}
	scanStart := 0
	if currentIndex >= 0 {
		scanStart = currentIndex + 1
	}

	for i, item := range items {
		path := strings.TrimSpace(item.Filename)
		if path == "" {
			continue
		}
		if i >= pathStart {
			overview.RemainingPaths = append(overview.RemainingPaths, path)
		}
		if i < scanStart {
			continue
		}
		if isStatusBanterPath(cfg, path) {
			if overview.Banter == "" {
				overview.Banter = readStatusBanterText(path)
				if overview.Banter == "" {
					overview.Banter = "queued"
				}
			}
			continue
		}
		overview.UpcomingSongs++
	}
	return overview, true
}

func isStatusBanterPath(cfg *config.Config, mediaPath string) bool {
	if cfg == nil {
		return false
	}
	base := strings.TrimSpace(cfg.TTS.DataDir)
	path := strings.TrimSpace(mediaPath)
	if base == "" || path == "" {
		return false
	}
	banterDir := filepath.Join(base, "banter")
	absBanter, errA := filepath.Abs(banterDir)
	absPath, errB := filepath.Abs(path)
	if errA != nil || errB != nil {
		return strings.HasPrefix(path, banterDir+string(os.PathSeparator)) || path == banterDir
	}
	return strings.HasPrefix(absPath, absBanter+string(os.PathSeparator)) || absPath == absBanter
}

func readStatusBanterText(audioPath string) string {
	data, err := statusReadFileFn(strings.TrimSpace(audioPath) + ".meta.json")
	if err != nil {
		return ""
	}
	var meta struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	text := strings.Join(strings.Fields(strings.TrimSpace(meta.Text)), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= statusBanterPreviewLimit {
		return text
	}
	return strings.TrimSpace(string(runes[:statusBanterPreviewLimit-3])) + "..."
}

func detectTTSStatus(cfg *config.Config) string {
	if cfg == nil {
		return "unavailable"
	}
	switch config.NormalizeTTSEngine(cfg.TTS.Engine) {
	case config.TTSEngineChatterbox:
		if ttsSocketResponsive(cfg.TTS.Socket) {
			return "chatterbox (warm)"
		}
		if pidFileRunning(pidFilePath(ttsPIDFileName)) {
			return "chatterbox (starting)"
		}
		if chatterboxInstalled(cfg) {
			return "chatterbox"
		}
		return "chatterbox (not installed)"
	default:
		return "system"
	}
}

func chatterboxInstalled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	dataDir := strings.TrimSpace(cfg.TTS.DataDir)
	if dataDir == "" {
		return false
	}
	pythonPath := filepath.Join(dataDir, "venv", "bin", "python")
	if _, err := os.Stat(pythonPath); err != nil {
		return false
	}
	daemonPath := filepath.Join(dataDir, "daemon.py")
	if _, err := os.Stat(daemonPath); err != nil {
		return false
	}
	return true
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

	fmt.Fprintf(w, "playlist: upcoming=%d preparing=%d\n", snapshot.Queue.Upcoming, snapshot.Queue.Preparing)
	if strings.TrimSpace(snapshot.Banter) != "" {
		fmt.Fprintf(w, "banter: %q\n", snapshot.Banter)
	} else {
		fmt.Fprintln(w, "banter: none queued")
	}
	if strings.TrimSpace(snapshot.Warning) != "" {
		fmt.Fprintf(w, "warning: %s\n", snapshot.Warning)
	}
	fmt.Fprintf(w, "controller: %s\n", snapshot.Controller)
	fmt.Fprintf(w, "tts: %s\n", snapshot.TTS)
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSONFlag, "json", false, "Return status as JSON")
	RootCmd.AddCommand(statusCmd)
}
