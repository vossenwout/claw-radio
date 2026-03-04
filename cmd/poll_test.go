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

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--json", "--timeout", "30ms")
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

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--json", "--timeout", "1s")
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if !strings.Contains(stdout, "queue_low") {
		t.Fatalf("stdout = %q, want queue_low event", stdout)
	}
}

func TestPollNonJSONHumanOutput(t *testing.T) {
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

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--json=false", "--timeout", (50 * time.Millisecond).String())
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if !strings.Contains(stdout, "banter_needed: SZA - Saturn") {
		t.Fatalf("stdout = %q, want human banter event", stdout)
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

	err, stdout, _ := executeCommandWithOutputForTest("poll", "--json", "--timeout", "1s")
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
