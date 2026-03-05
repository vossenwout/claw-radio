package station

import (
	"reflect"
	"testing"
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
