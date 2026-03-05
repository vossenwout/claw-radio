package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestStopClearsStationStateAndCache(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "station.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "song.opus"), []byte("audio"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	cfg := baseEngineTestConfig(tmp)
	cfg.Station = config.StationConfig{StateDir: stateDir, CacheDir: cacheDir}

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("stop")
	if err != nil {
		t.Fatalf("stop command failed: %v", err)
	}
	if !strings.Contains(stdout, "stopped and reset") {
		t.Fatalf("stdout = %q, want stop+reset message", stdout)
	}

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("cache dir should be removed, err=%v", err)
	}
}
