package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/station"
)

func TestPollReturnsTimeoutEvent(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{Station: config.StationConfig{StateDir: stateDir}}
	restoreRuntime := withRunningRuntimeForPollTests(t)
	defer restoreRuntime()

	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	defer func() { loadConfigFn = origLoad }()

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--timeout", "30ms")
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &event); err != nil {
		t.Fatalf("parse poll json: %v", err)
	}
	if event["event"] != "timeout" {
		t.Fatalf("event = %v, want timeout", event["event"])
	}
	if event["action"] != "wait" {
		t.Fatalf("action = %v, want wait", event["action"])
	}
}

func TestPollReturnsBufferingCueWhenRadioBuffering(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		MPV:     config.MPVConfig{Socket: filepath.Join(stateDir, "mock.sock")},
		Station: config.StationConfig{StateDir: stateDir},
	}
	restoreRuntime := withRunningRuntimeForPollTests(t)
	defer restoreRuntime()

	stationJSON := `{"label":"","seeds":["Party in the usa - miley cyrus"]}`
	if err := os.WriteFile(filepath.Join(stateDir, "station.json"), []byte(stationJSON), 0o644); err != nil {
		t.Fatalf("write station.json: %v", err)
	}

	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	defer func() { loadConfigFn = origLoad }()

	origDial := dialStatusMPVClientFn
	dialStatusMPVClientFn = func(socketPath string) (statusMPVClient, error) {
		return &fakeStatusMPVClient{
			props: map[string]json.RawMessage{
				"pause":       mustRawJSON(t, false),
				"media-title": mustRawJSON(t, ""),
				"time-pos":    mustRawJSON(t, 0),
				"duration":    mustRawJSON(t, 0),
				"volume":      mustRawJSON(t, 100),
				"playlist":    mustRawJSON(t, []map[string]interface{}{}),
			},
			playlistCount: 0,
		}, nil
	}
	defer func() { dialStatusMPVClientFn = origDial }()

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--timeout", "30ms")
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &event); err != nil {
		t.Fatalf("parse poll json: %v", err)
	}
	if event["event"] != "buffering" {
		t.Fatalf("event = %v, want buffering", event["event"])
	}
	if event["action"] != "wait" {
		t.Fatalf("action = %v, want wait", event["action"])
	}
}

func TestPollReturnsQueuedEvent(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{Station: config.StationConfig{StateDir: stateDir}}
	restoreRuntime := withRunningRuntimeForPollTests(t)
	defer restoreRuntime()

	store := station.NewAgentEventStore(stateDir)
	if err := store.Append(station.AgentEvent{Event: "queue_low", Count: 1, Depth: 5}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	defer func() { loadConfigFn = origLoad }()

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--timeout", "1s")
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &event); err != nil {
		t.Fatalf("parse poll json: %v", err)
	}
	if event["event"] != "queue_low" {
		t.Fatalf("event = %v, want queue_low", event["event"])
	}
	if event["action"] != "add_songs" {
		t.Fatalf("action = %v, want add_songs", event["action"])
	}
}

func TestPollReturnsBanterNeededJSONPayload(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{Station: config.StationConfig{StateDir: stateDir}}
	restoreRuntime := withRunningRuntimeForPollTests(t)
	defer restoreRuntime()

	store := station.NewAgentEventStore(stateDir)
	if err := store.Append(station.AgentEvent{
		Event: "banter_needed",
		NextSong: &station.AgentSong{
			Artist: "SZA",
			Title:  "Saturn",
		},
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	defer func() { loadConfigFn = origLoad }()

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--timeout", (50 * time.Millisecond).String())
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if !strings.Contains(stdout, "\"event\":\"banter_needed\"") {
		t.Fatalf("stdout = %q, want banter_needed json event", stdout)
	}
	if strings.Contains(stdout, "\"path\":") {
		t.Fatalf("stdout should not expose internal path: %q", stdout)
	}
	if !strings.Contains(stdout, "\"upcoming_song\":\"SZA - Saturn\"") {
		t.Fatalf("stdout = %q, want upcoming_song display", stdout)
	}
	if !strings.Contains(stdout, "\"action\":\"speak\"") {
		t.Fatalf("stdout = %q, want speak action", stdout)
	}
}

func TestPollReturnsEngineStoppedWhenRadioNotRunning(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{Station: config.StationConfig{StateDir: stateDir}}

	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	defer func() { loadConfigFn = origLoad }()

	origPIDBase := pidBaseDir
	pidBaseDir = t.TempDir()
	defer func() { pidBaseDir = origPIDBase }()

	origRunning := statusProcessRunningFn
	statusProcessRunningFn = func(int) bool { return false }
	defer func() { statusProcessRunningFn = origRunning }()

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--timeout", "1s")
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &event); err != nil {
		t.Fatalf("parse poll json: %v", err)
	}
	if event["event"] != "engine_stopped" {
		t.Fatalf("event = %v, want engine_stopped", event["event"])
	}
	if event["action"] != "restart" {
		t.Fatalf("action = %v, want restart", event["action"])
	}
}

func withRunningRuntimeForPollTests(t *testing.T) func() {
	t.Helper()

	origPIDBase := pidBaseDir
	origRunning := statusProcessRunningFn

	pidDir := t.TempDir()
	pidBaseDir = pidDir
	statusProcessRunningFn = func(pid int) bool {
		return pid == 111 || pid == 222
	}

	writePID := func(name string, pid int) {
		if err := os.WriteFile(filepath.Join(pidDir, name), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
			t.Fatalf("write pid file %s: %v", name, err)
		}
	}
	writePID(mpvPIDFileName, 111)
	writePID(controllerPIDFile, 222)

	return func() {
		pidBaseDir = origPIDBase
		statusProcessRunningFn = origRunning
	}
}
