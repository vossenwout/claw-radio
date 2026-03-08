package station

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
)

type fakeFillQueueMPVClient struct {
	playlistCount int
	idleActive    bool
	loadModes     []string
	props         map[string]json.RawMessage
	playlistPaths []string
}

func (f *fakeFillQueueMPVClient) Close() error { return nil }

func (f *fakeFillQueueMPVClient) PlaylistCount() (int, error) {
	return f.playlistCount, nil
}

func (f *fakeFillQueueMPVClient) PlaylistPaths() ([]string, error) {
	if f.playlistPaths == nil {
		return nil, nil
	}
	return append([]string(nil), f.playlistPaths...), nil
}

func (f *fakeFillQueueMPVClient) LoadFile(path, mode string) error {
	f.loadModes = append(f.loadModes, mode)
	return nil
}

func (f *fakeFillQueueMPVClient) Get(prop string) (json.RawMessage, error) {
	if raw, ok := f.props[prop]; ok {
		return raw, nil
	}
	if prop == "idle-active" {
		data, _ := json.Marshal(f.idleActive)
		return data, nil
	}
	return nil, errors.New("property not found")
}

func (f *fakeFillQueueMPVClient) Events() <-chan map[string]interface{} {
	return make(chan map[string]interface{})
}

type fakeFillQueueProvider struct {
	path       string
	resolved   []string
	resolveErr error
}

func (f fakeFillQueueProvider) Name() string { return "fake" }

func (f *fakeFillQueueProvider) Resolve(seed, cacheDir string) (string, error) {
	f.resolved = append(f.resolved, seed)
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.path, nil
}

func TestFillQueueUsesAppendPlayWhenIdleActive(t *testing.T) {
	stateDir := t.TempDir()
	st := &Station{StateDir: stateDir}
	st.SetSeeds([]string{"Fergie - Glamorous"}, "")
	if err := st.Save(); err != nil {
		t.Fatalf("save station seed state: %v", err)
	}

	fakeClient := &fakeFillQueueMPVClient{playlistCount: 1, idleActive: true}
	svc := &service{
		cfg: &config.Config{Station: config.StationConfig{
			StateDir:   stateDir,
			CacheDir:   filepath.Join(stateDir, "cache"),
			QueueDepth: 2,
		}},
		st:     &Station{StateDir: stateDir},
		client: fakeClient,
		prov:   &fakeFillQueueProvider{path: filepath.Join(stateDir, "cache", "fergie.opus")},
	}

	if err := svc.fillQueue(); err != nil {
		t.Fatalf("fillQueue failed: %v", err)
	}

	if len(fakeClient.loadModes) != 1 {
		t.Fatalf("load call count = %d, want 1", len(fakeClient.loadModes))
	}
	if fakeClient.loadModes[0] != "append-play" {
		t.Fatalf("load mode = %q, want append-play", fakeClient.loadModes[0])
	}
}

func TestFillQueueUsesUpcomingCountInsteadOfHistoricalPlaylistCount(t *testing.T) {
	stateDir := t.TempDir()
	st := &Station{StateDir: stateDir}
	st.SetSeeds([]string{"SZA - Saturn"}, "")
	if err := st.Save(); err != nil {
		t.Fatalf("save station seed state: %v", err)
	}

	currentPath := filepath.Join(stateDir, "current.opus")
	fakeClient := &fakeFillQueueMPVClient{
		playlistCount: 3,
		props: map[string]json.RawMessage{
			"path": mustRaw(currentPath),
			"playlist": mustRaw([]map[string]interface{}{
				{"filename": filepath.Join(stateDir, "played-intro.aiff")},
				{"filename": filepath.Join(stateDir, "played-song.opus")},
				{"filename": currentPath, "current": true, "playing": true},
			}),
		},
	}
	provider := &fakeFillQueueProvider{path: filepath.Join(stateDir, "cache", "saturn.opus")}
	svc := &service{
		cfg: &config.Config{Station: config.StationConfig{
			StateDir:   stateDir,
			CacheDir:   filepath.Join(stateDir, "cache"),
			QueueDepth: 1,
		}},
		st:     &Station{StateDir: stateDir},
		client: fakeClient,
		prov:   provider,
	}

	if err := svc.fillQueue(); err != nil {
		t.Fatalf("fillQueue failed: %v", err)
	}

	if len(provider.resolved) != 1 {
		t.Fatalf("resolved count = %d, want 1", len(provider.resolved))
	}
	if len(fakeClient.loadModes) != 1 {
		t.Fatalf("load call count = %d, want 1", len(fakeClient.loadModes))
	}
}

func TestPrefetchOneSeedForStartupResolvesWithoutAdvancingSeedIndex(t *testing.T) {
	stateDir := t.TempDir()
	provider := &fakeFillQueueProvider{path: filepath.Join(stateDir, "cache", "song.opus")}
	seedState := &Station{StateDir: stateDir}
	seedState.SetSeeds([]string{"A - B", "C - D"}, "")
	if err := seedState.Save(); err != nil {
		t.Fatalf("save station seed state: %v", err)
	}
	st := &Station{StateDir: stateDir, seedIndex: 1}

	svc := &service{
		cfg:  &config.Config{Station: config.StationConfig{StateDir: stateDir, CacheDir: filepath.Join(stateDir, "cache")}},
		st:   st,
		prov: provider,
	}

	prefetched, err := svc.prefetchOneSeedForStartup()
	if err != nil {
		t.Fatalf("prefetchOneSeedForStartup failed: %v", err)
	}
	if prefetched == nil {
		t.Fatal("prefetchOneSeedForStartup returned nil, want prefetched song")
	}
	if prefetched.AudioPath == "" {
		t.Fatal("prefetched audio path is empty")
	}

	if len(provider.resolved) != 1 {
		t.Fatalf("resolved count = %d, want 1", len(provider.resolved))
	}
	if provider.resolved[0] != "A - B" {
		t.Fatalf("resolved seed = %q, want %q", provider.resolved[0], "A - B")
	}
	if st.seedIndex != 1 {
		t.Fatalf("seedIndex = %d, want 1", st.seedIndex)
	}
}
