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

func TestPlaylistAddWritesProvidedSongs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "playlist", "add", `["A - B", "C - D"]`); err != nil {
		t.Fatalf("playlist add command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if !reflect.DeepEqual(station.Seeds, []string{"A - B", "C - D"}) {
		t.Fatalf("playlist songs mismatch: got %v", station.Seeds)
	}
}

func TestPlaylistAddDeduplicatesExistingSong(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "playlist", "add", `["A - B", "C - D"]`); err != nil {
		t.Fatalf("initial playlist add command failed: %v", err)
	}
	if err := executeCommandForTest(t, "playlist", "add", `["A - B"]`); err != nil {
		t.Fatalf("second playlist add command failed: %v", err)
	}

	station := readStationJSONForTest(t, stateDir)
	if !reflect.DeepEqual(station.Seeds, []string{"A - B", "C - D"}) {
		t.Fatalf("playlist songs mismatch after dedupe: got %v", station.Seeds)
	}
}

func TestPlaylistViewHumanShowsSongs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "playlist", "add", `["A - B", "C - D"]`); err != nil {
		t.Fatalf("playlist add command failed: %v", err)
	}

	err, stdout, _ := executeCommandWithOutputForTest("playlist", "view")
	if err != nil {
		t.Fatalf("playlist view command failed: %v", err)
	}

	if !strings.Contains(stdout, "Playlist (2 songs):") {
		t.Fatalf("stdout = %q, want playlist header", stdout)
	}
	if !strings.Contains(stdout, "1. A - B") || !strings.Contains(stdout, "2. C - D") {
		t.Fatalf("stdout = %q, want numbered song rows", stdout)
	}
}

func TestPlaylistViewHumanWhenEmpty(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("playlist", "view")
	if err != nil {
		t.Fatalf("playlist view command failed: %v", err)
	}
	if !strings.Contains(stdout, "Playlist is empty") {
		t.Fatalf("stdout = %q, want empty message", stdout)
	}
}

func TestPlaylistViewJSONReturnsSongArray(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "playlist", "add", `["A - B", "C - D"]`); err != nil {
		t.Fatalf("playlist add command failed: %v", err)
	}

	err, stdout, _ := executeCommandWithOutputForTest("playlist", "view", "--json")
	if err != nil {
		t.Fatalf("playlist view --json command failed: %v", err)
	}

	var songs []string
	if err := json.Unmarshal([]byte(stdout), &songs); err != nil {
		t.Fatalf("parse playlist json output: %v", err)
	}
	if !reflect.DeepEqual(songs, []string{"A - B", "C - D"}) {
		t.Fatalf("playlist json mismatch: got %v", songs)
	}
}

func TestPlaylistResetClearsSongs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	if err := executeCommandForTest(t, "playlist", "add", `["A - B"]`); err != nil {
		t.Fatalf("playlist add command failed: %v", err)
	}

	err, stdout, _ := executeCommandWithOutputForTest("playlist", "reset")
	if err != nil {
		t.Fatalf("playlist reset command failed: %v", err)
	}
	if !strings.Contains(stdout, "Playlist reset") {
		t.Fatalf("stdout = %q, want reset confirmation", stdout)
	}

	station := readStationJSONForTest(t, stateDir)
	if len(station.Seeds) != 0 {
		t.Fatalf("playlist should be empty after reset: %v", station.Seeds)
	}
}

func TestPlaylistAddInvalidJSONExitsOneWithParseError(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	err := executeCommandForTest(t, "playlist", "add", "not-json")
	assertExitCode(t, err, 1)
	if !strings.Contains(strings.ToLower(err.Error()), "parse playlist json") {
		t.Fatalf("expected parse error message, got %q", err)
	}
}

func TestPlaylistAddNonArrayJSONExitsOne(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	err := executeCommandForTest(t, "playlist", "add", "42")
	assertExitCode(t, err, 1)
}

func TestPlaylistAddWithoutArgumentExitsTwoWithUsage(t *testing.T) {
	stateDir := t.TempDir()
	cfg := &config.Config{
		Station: config.StationConfig{
			StateDir: stateDir,
		},
	}
	restore := withPlaylistTestHooks(cfg)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("playlist", "add")
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

func withPlaylistTestHooks(cfg *config.Config) func() {
	origLoad := loadConfigFn
	origViewJSON := playlistViewJSONFlag
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	playlistViewJSONFlag = false

	return func() {
		loadConfigFn = origLoad
		playlistViewJSONFlag = origViewJSON
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
