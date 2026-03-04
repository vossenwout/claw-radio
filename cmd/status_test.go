package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestStatusJSONReportsStoppedEngineWithoutError(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(tmp, "missing.sock"),
		},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket: filepath.Join(tmp, "missing-tts.sock"),
		},
	}

	restore := withStatusTestHooks(cfg, tmp, nil, nil, nil)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Engine != "stopped" {
		t.Fatalf("engine = %q, want %q", out.Engine, "stopped")
	}
}

func TestStatusJSONReportsRunningEngineAndPlaybackFields(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(tmp, "mock.sock"),
		},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket: filepath.Join(tmp, "missing-tts.sock"),
		},
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 101)

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "Britney Spears - Oops! I Did It Again"),
			"time-pos":    mustRawJSON(t, 47.3),
			"duration":    mustRawJSON(t, 211.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool {
		return pid == 101
	})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Engine != "running" {
		t.Fatalf("engine = %q, want %q", out.Engine, "running")
	}
	if out.Playback == nil {
		t.Fatalf("playback missing from output: %s", stdout)
	}
	if out.Playback.Title != "Britney Spears - Oops! I Did It Again" {
		t.Fatalf("playback.title = %q", out.Playback.Title)
	}
	if out.Playback.State != "playing" {
		t.Fatalf("playback.state = %q, want %q", out.Playback.State, "playing")
	}
	if out.Playback.Volume != 30 {
		t.Fatalf("playback.volume = %v, want 30", out.Playback.Volume)
	}
}

func TestStatusJSONUsesMetadataTitleWhenMediaTitleLooksLikeFilename(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(tmp, "mock.sock"),
		},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket: filepath.Join(tmp, "missing-tts.sock"),
		},
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 101)

	metadata := mustRawJSON(t, map[string]interface{}{
		"artist": "Kendrick Lamar",
		"title":  "Money Trees",
	})

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "tvTRZJ-4EyI.opus"),
			"metadata":    metadata,
			"time-pos":    mustRawJSON(t, 47.3),
			"duration":    mustRawJSON(t, 211.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool {
		return pid == 101
	})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Playback == nil {
		t.Fatalf("playback missing from output: %s", stdout)
	}
	if out.Playback.Title != "Kendrick Lamar - Money Trees" {
		t.Fatalf("playback.title = %q, want %q", out.Playback.Title, "Kendrick Lamar - Money Trees")
	}
}

func TestStatusJSONTrimsMediaFilenameFallback(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(tmp, "mock.sock"),
		},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 102)

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "tvTRZJ-4EyI.opus"),
			"time-pos":    mustRawJSON(t, 1.0),
			"duration":    mustRawJSON(t, 211.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool {
		return pid == 102
	})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Playback == nil {
		t.Fatalf("playback missing from output: %s", stdout)
	}
	if out.Playback.Title != "tvTRZJ-4EyI" {
		t.Fatalf("playback.title = %q, want %q", out.Playback.Title, "tvTRZJ-4EyI")
	}
}

func TestStatusJSONUsesSidecarSeedTitleFallback(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	audioPath := filepath.Join(cacheDir, "tvTRZJ-4EyI.opus")
	if err := os.WriteFile(audioPath+".meta.json", mustRawJSON(t, map[string]string{
		"seed":    "Kendrick Lamar - Money Trees",
		"display": "Kendrick Lamar - Money Trees",
	}), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: filepath.Join(tmp, "mock.sock")},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 103)

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "tvTRZJ-4EyI.opus"),
			"path":        mustRawJSON(t, audioPath),
			"time-pos":    mustRawJSON(t, 10.0),
			"duration":    mustRawJSON(t, 211.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool { return pid == 103 })
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Playback == nil {
		t.Fatalf("playback missing from output: %s", stdout)
	}
	if out.Playback.Title != "Kendrick Lamar - Money Trees" {
		t.Fatalf("playback.title = %q, want %q", out.Playback.Title, "Kendrick Lamar - Money Trees")
	}
}

func TestStatusJSONUsesYouTubeOEmbedFallback(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: filepath.Join(tmp, "mock.sock")},
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 104)

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "tvTRZJ-4EyI.opus"),
			"time-pos":    mustRawJSON(t, 10.0),
			"duration":    mustRawJSON(t, 211.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool { return pid == 104 })
	defer restore()

	origOEmbed := fetchYouTubeOEmbedFn
	fetchYouTubeOEmbedFn = func(videoID string) (string, string, error) {
		if videoID != "tvTRZJ-4EyI" {
			t.Fatalf("video id = %q, want %q", videoID, "tvTRZJ-4EyI")
		}
		return "Kendrick Lamar", "Money Trees", nil
	}
	defer func() { fetchYouTubeOEmbedFn = origOEmbed }()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Playback == nil {
		t.Fatalf("playback missing from output: %s", stdout)
	}
	if out.Playback.Title != "Kendrick Lamar - Money Trees" {
		t.Fatalf("playback.title = %q, want %q", out.Playback.Title, "Kendrick Lamar - Money Trees")
	}
}

func TestStatusJSONIncludesStationSeedCount(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	seeds := make([]string, 48)
	for i := range seeds {
		seeds[i] = fmt.Sprintf("Artist %d - Song %d", i, i)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"label": "2000s bubblegum pop",
		"seeds": seeds,
	})
	if err := os.WriteFile(filepath.Join(stateDir, "station.json"), data, 0o644); err != nil {
		t.Fatalf("write station.json: %v", err)
	}

	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir:   stateDir,
			QueueDepth: 5,
		},
	}

	restore := withStatusTestHooks(cfg, tmp, nil, nil, nil)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Station == nil {
		t.Fatalf("station missing from output: %s", stdout)
	}
	if out.Station.Seeds != 48 {
		t.Fatalf("station.seeds = %d, want 48", out.Station.Seeds)
	}
}

func TestStatusJSONReportsWarmTTSWhenSocketResponds(t *testing.T) {
	tmp := t.TempDir()
	socketPath := makePlaybackSocketPath(t, "status-tts-warm")
	stopWarm := startWarmSocketForTest(t, socketPath)
	defer stopWarm()

	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket: socketPath,
		},
	}

	restore := withStatusTestHooks(cfg, tmp, nil, nil, nil)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.TTS != "warm" {
		t.Fatalf("tts = %q, want %q", out.TTS, "warm")
	}
}

func TestStatusJSONReportsSystemTTSWhenFallbackConfigured(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir:   filepath.Join(tmp, "state"),
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket:         filepath.Join(tmp, "missing.sock"),
			FallbackBinary: "/usr/bin/say",
		},
	}

	restore := withStatusTestHooks(cfg, tmp, nil, nil, nil)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.TTS != "system" {
		t.Fatalf("tts = %q, want %q", out.TTS, "system")
	}
}

func TestStatusJSONMatchesSchema(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"label": "Night Drive",
		"seeds": repeatedStringSlice("A - B", 48),
	})
	if err := os.WriteFile(filepath.Join(stateDir, "station.json"), data, 0o644); err != nil {
		t.Fatalf("write station.json: %v", err)
	}

	writePIDForTest(t, filepath.Join(tmp, mpvPIDFileName), 500)
	writePIDForTest(t, filepath.Join(tmp, controllerPIDFile), 700)

	ttsSocket := makePlaybackSocketPath(t, "status-tts-schema")
	stopWarm := startWarmSocketForTest(t, ttsSocket)
	defer stopWarm()

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(tmp, "mock.sock"),
		},
		Station: config.StationConfig{
			StateDir:   stateDir,
			QueueDepth: 5,
		},
		TTS: config.TTSConfig{
			Socket: ttsSocket,
		},
	}

	client := &fakeStatusMPVClient{
		props: map[string]json.RawMessage{
			"pause":       mustRawJSON(t, false),
			"media-title": mustRawJSON(t, "Midnight City"),
			"time-pos":    mustRawJSON(t, 12.5),
			"duration":    mustRawJSON(t, 244.0),
			"volume":      mustRawJSON(t, 30),
		},
		playlistCount: 4,
	}

	restore := withStatusTestHooks(cfg, tmp, client, nil, func(pid int) bool {
		return pid == 500 || pid == 700
	})
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &top); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}
	for _, key := range []string{"engine", "station", "playback", "queue", "controller", "tts"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("missing %q in schema: %s", key, stdout)
		}
	}

	out := decodeStatusJSONForTest(t, stdout)
	if out.Engine != "running" || out.Controller != "running" || out.TTS != "warm" {
		t.Fatalf("unexpected top-level values: %#v", out)
	}
	if out.Station == nil || out.Station.Seeds != 48 || out.Station.Label != "Night Drive" {
		t.Fatalf("unexpected station payload: %#v", out.Station)
	}
	if out.Playback == nil || out.Playback.State == "" || out.Playback.Title == "" {
		t.Fatalf("unexpected playback payload: %#v", out.Playback)
	}
}

func repeatedStringSlice(value string, count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}

type statusOutputForTest struct {
	Engine     string                 `json:"engine"`
	Station    *statusStationForTest  `json:"station,omitempty"`
	Playback   *statusPlaybackForTest `json:"playback,omitempty"`
	Queue      statusQueueForTest     `json:"queue"`
	Controller string                 `json:"controller"`
	TTS        string                 `json:"tts"`
}

type statusStationForTest struct {
	Label string `json:"label"`
	Seeds int    `json:"seeds"`
}

type statusPlaybackForTest struct {
	State    string  `json:"state"`
	Title    string  `json:"title"`
	TimePos  float64 `json:"time_pos"`
	Duration float64 `json:"duration"`
	Volume   float64 `json:"volume"`
}

type statusQueueForTest struct {
	Count int `json:"count"`
	Depth int `json:"depth"`
}

type fakeStatusMPVClient struct {
	props         map[string]json.RawMessage
	getErr        map[string]error
	playlistCount int
	playlistErr   error
}

func (f *fakeStatusMPVClient) Close() error {
	return nil
}

func (f *fakeStatusMPVClient) Get(prop string) (json.RawMessage, error) {
	if err, ok := f.getErr[prop]; ok {
		return nil, err
	}
	raw, ok := f.props[prop]
	if !ok {
		return nil, fmt.Errorf("property %q not found", prop)
	}
	return raw, nil
}

func (f *fakeStatusMPVClient) PlaylistCount() (int, error) {
	if f.playlistErr != nil {
		return 0, f.playlistErr
	}
	return f.playlistCount, nil
}

func withStatusTestHooks(
	cfg *config.Config,
	pidDir string,
	client statusMPVClient,
	dialErr error,
	processRunning func(int) bool,
) func() {
	origLoad := loadConfigFn
	origDial := dialStatusMPVClientFn
	origPIDBase := pidBaseDir
	origProcRunning := statusProcessRunningFn
	origOEmbed := fetchYouTubeOEmbedFn

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	dialStatusMPVClientFn = func(string) (statusMPVClient, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		if client == nil {
			return nil, fmt.Errorf("no client")
		}
		return client, nil
	}
	pidBaseDir = pidDir
	if processRunning != nil {
		statusProcessRunningFn = processRunning
	} else {
		statusProcessRunningFn = func(int) bool { return false }
	}
	fetchYouTubeOEmbedFn = func(string) (string, string, error) { return "", "", fmt.Errorf("disabled in tests") }

	return func() {
		loadConfigFn = origLoad
		dialStatusMPVClientFn = origDial
		pidBaseDir = origPIDBase
		statusProcessRunningFn = origProcRunning
		fetchYouTubeOEmbedFn = origOEmbed
	}
}

func writePIDForTest(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatalf("write pid file %s: %v", path, err)
	}
}

func mustRawJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return data
}

func decodeStatusJSONForTest(t *testing.T, raw string) statusOutputForTest {
	t.Helper()
	var out statusOutputForTest
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal status json: %v (raw=%q)", err, raw)
	}
	return out
}

func startWarmSocketForTest(t *testing.T, socketPath string) func() {
	t.Helper()

	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.AcceptUnix()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	return func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
		_ = os.Remove(socketPath)
	}
}
