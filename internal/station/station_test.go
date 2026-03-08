package station

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestSetSeedsPickSeedInOrder(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.SetSeeds([]string{"A", "B", "C"}, "label")

	got := []string{st.PickSeed(), st.PickSeed(), st.PickSeed()}
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PickSeed order mismatch: got %v want %v", got, want)
	}
}

func TestPickSeedWrapsAround(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.SetSeeds([]string{"A", "B"}, "label")

	got := []string{st.PickSeed(), st.PickSeed(), st.PickSeed(), st.PickSeed()}
	want := []string{"A", "B", "A", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PickSeed wrap mismatch: got %v want %v", got, want)
	}
}

func TestAppendSeedsDeduplicates(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.AppendSeeds([]string{"A", "B"})
	st.AppendSeeds([]string{"B", "C"})

	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(st.Seeds, want) {
		t.Fatalf("Seeds mismatch: got %v want %v", st.Seeds, want)
	}
}

func TestRemoveSeedRemovesMatchingEntry(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.AppendSeeds([]string{"A - B", "C - D"})

	if removed := st.RemoveSeed("A - B"); !removed {
		t.Fatal("RemoveSeed returned false, want true")
	}
	if !reflect.DeepEqual(st.Seeds, []string{"C - D"}) {
		t.Fatalf("Seeds mismatch after RemoveSeed: got %v", st.Seeds)
	}
}

func TestMarkPlayedDedupeByVideoID(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.MarkPlayed("vid123", "song key")

	if !st.AlreadyPlayed("vid123", "other") {
		t.Fatalf("expected AlreadyPlayed true for matching video ID")
	}
}

func TestMarkPlayedDedupeBySongKey(t *testing.T) {
	t.Parallel()

	st := &Station{}
	st.MarkPlayed("vid123", "song key")

	if !st.AlreadyPlayed("vid999", "song key") {
		t.Fatalf("expected AlreadyPlayed true for matching song key")
	}
}

func TestLoadFreshNoFiles(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	st, err := Load(stateDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(st.Seeds) != 0 {
		t.Fatalf("expected empty seeds, got %v", st.Seeds)
	}
	if len(st.PlayedVideoIDs) != 0 {
		t.Fatalf("expected empty played video IDs, got %v", st.PlayedVideoIDs)
	}
	if len(st.PlayedSongKeys) != 0 {
		t.Fatalf("expected empty played song keys, got %v", st.PlayedSongKeys)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	st := &Station{StateDir: stateDir}
	st.SetSeeds([]string{"A", "B", "C"}, "2000s pop")
	st.MarkPlayed("vid123", "Song Key")
	st.MarkPlayed("vid456", "Another Song")

	if err := st.Save(); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	reloaded, err := Load(stateDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if reloaded.Label != "2000s pop" {
		t.Fatalf("label mismatch: got %q", reloaded.Label)
	}
	if !reflect.DeepEqual(reloaded.Seeds, []string{"A", "B", "C"}) {
		t.Fatalf("seeds mismatch: got %v", reloaded.Seeds)
	}
	if !reflect.DeepEqual(reloaded.PlayedVideoIDs, []string{"vid123", "vid456"}) {
		t.Fatalf("played video IDs mismatch: got %v", reloaded.PlayedVideoIDs)
	}
	if !reflect.DeepEqual(reloaded.PlayedSongKeys, []string{"song key", "another song"}) {
		t.Fatalf("played song keys mismatch: got %v", reloaded.PlayedSongKeys)
	}
}

func TestBuildPlaylistSnapshotSkipsCurrentSongAndMarksReadySongs(t *testing.T) {
	stateDir := t.TempDir()
	currentPath := filepath.Join(stateDir, "current.opus")
	nextPath := filepath.Join(stateDir, "next.opus")
	for _, entry := range []struct {
		path string
		seed string
	}{
		{path: currentPath, seed: "Current Artist - Current Song"},
		{path: nextPath, seed: "SZA - Saturn"},
	} {
		data, _ := json.Marshal(map[string]string{"seed": entry.seed, "display": entry.seed})
		if err := os.WriteFile(entry.path+".meta.json", data, 0o644); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
	}

	snapshot := BuildPlaylistSnapshot(
		&config.Config{},
		[]string{"Current Artist - Current Song", "SZA - Saturn", "Kendrick Lamar - Alright"},
		currentPath,
		[]string{currentPath, nextPath},
	)

	if len(snapshot.Songs) != 2 {
		t.Fatalf("song count = %d, want 2", len(snapshot.Songs))
	}
	if snapshot.Ready != 1 {
		t.Fatalf("ready = %d, want 1", snapshot.Ready)
	}
	if snapshot.Preparing != 1 {
		t.Fatalf("preparing = %d, want 1", snapshot.Preparing)
	}
	if snapshot.Songs[0].Seed != "SZA - Saturn" || snapshot.Songs[0].Status != PlaylistSongReady {
		t.Fatalf("first song = %#v, want ready SZA - Saturn", snapshot.Songs[0])
	}
	if snapshot.Songs[1].Seed != "Kendrick Lamar - Alright" || snapshot.Songs[1].Status != PlaylistSongPreparing {
		t.Fatalf("second song = %#v, want preparing Kendrick Lamar - Alright", snapshot.Songs[1])
	}
}
