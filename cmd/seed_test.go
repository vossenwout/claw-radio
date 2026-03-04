package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestSeedWritesProvidedSeeds(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "seed", `["A - B", "C - D"]`); err != nil {
		t.Fatalf("seed command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if !reflect.DeepEqual(station.Seeds, []string{"A - B", "C - D"}) {
		t.Fatalf("seed list mismatch: got %v", station.Seeds)
	}
}

func TestSeedClearsLabelOnReplace(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "seed", `["A - B"]`); err != nil {
		t.Fatalf("seed command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if station.Label != "" {
		t.Fatalf("label mismatch: got %q want empty", station.Label)
	}
}

func TestSeedAppendAddsWithoutReplacingExistingSeeds(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "seed", `["A", "B", "C", "D", "E"]`); err != nil {
		t.Fatalf("initial seed command failed: %v", err)
	}
	if err := executeCommandForTest(t, "seed", `["X - Y"]`, "--append"); err != nil {
		t.Fatalf("append seed command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if got := len(station.Seeds); got != 6 {
		t.Fatalf("seed count after append = %d, want 6", got)
	}
}

func TestSeedAppendDeduplicatesExistingSeed(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "seed", `["A", "B"]`); err != nil {
		t.Fatalf("initial seed command failed: %v", err)
	}
	if err := executeCommandForTest(t, "seed", `["B"]`, "--append"); err != nil {
		t.Fatalf("append seed command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if !reflect.DeepEqual(station.Seeds, []string{"A", "B"}) {
		t.Fatalf("seed list mismatch after dedupe append: got %v", station.Seeds)
	}
}

func TestSeedInvalidJSONExitsOneWithParseError(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	err := executeCommandForTest(t, "seed", "not-json")
	assertExitCode(t, err, 1)
	if !strings.Contains(strings.ToLower(err.Error()), "parse seed json") {
		t.Fatalf("expected parse error message, got %q", err)
	}
}

func TestSeedNonArrayJSONExitsOne(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	err := executeCommandForTest(t, "seed", "42")
	assertExitCode(t, err, 1)
}

func TestSeedWithoutArgumentExitsTwoWithUsage(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withSeedTestHooks(cfg)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("seed")
	assertExitCode(t, err, 2)
	output := stdout + stderr
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("usage output missing: %q", output)
	}
}

type stationFileForSeedTest struct {
	Label string   `json:"label"`
	Seeds []string `json:"seeds"`
}

func readStationJSONForTest(t *testing.T, stateDir string) stationFileForSeedTest {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(stateDir, "station.json"))
	if err != nil {
		t.Fatalf("read station.json: %v", err)
	}

	var station stationFileForSeedTest
	if err := json.Unmarshal(data, &station); err != nil {
		t.Fatalf("parse station.json: %v", err)
	}
	return station
}

func withSeedTestHooks(cfg *config.Config) func() {
	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}

	return func() {
		loadConfigFn = origLoad
	}
}

func executeCommandWithOutputForTest(args ...string) (error, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs(args)
	return Execute(), stdout.String(), stderr.String()
}
