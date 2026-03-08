package station

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
)

type fakeControllerMPVClient struct {
	props map[string]json.RawMessage
}

func (f *fakeControllerMPVClient) Close() error                     { return nil }
func (f *fakeControllerMPVClient) PlaylistCount() (int, error)      { return 2, nil }
func (f *fakeControllerMPVClient) PlaylistPaths() ([]string, error) { return []string{}, nil }
func (f *fakeControllerMPVClient) LoadFile(path, mode string) error { return nil }
func (f *fakeControllerMPVClient) Events() <-chan map[string]interface{} {
	return make(chan map[string]interface{})
}
func (f *fakeControllerMPVClient) Get(prop string) (json.RawMessage, error) {
	return f.props[prop], nil
}

func TestHandleTrackStartedEmitsBanterNeeded(t *testing.T) {
	stateDir := t.TempDir()
	nextPath := filepath.Join(stateDir, "next.opus")
	meta := map[string]string{"artist": "SZA", "title": "Saturn", "display": "SZA - Saturn"}
	metaRaw, _ := json.Marshal(meta)
	if err := os.WriteFile(nextPath+".meta.json", metaRaw, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	playlist := []map[string]interface{}{
		{"filename": filepath.Join(stateDir, "current.opus"), "current": true, "playing": true},
		{"filename": nextPath},
	}
	playlistRaw, _ := json.Marshal(playlist)

	fake := &fakeControllerMPVClient{props: map[string]json.RawMessage{
		"path":     mustRaw(filepath.Join(stateDir, "current.opus")),
		"playlist": playlistRaw,
	}}

	svc := &service{
		cfg:    &config.Config{Station: config.StationConfig{StateDir: stateDir, QueueDepth: 5}},
		st:     &Station{StateDir: stateDir},
		client: fake,
		events: NewAgentEventStore(stateDir),
	}

	if err := svc.handleTrackStarted(); err != nil {
		t.Fatalf("handleTrackStarted error: %v", err)
	}

	event, err := svc.events.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if event.Event != "banter_needed" {
		t.Fatalf("event=%q, want banter_needed", event.Event)
	}
	if event.NextSong == nil || event.NextSong.Artist != "SZA" || event.NextSong.Title != "Saturn" {
		t.Fatalf("next_song mismatch: %#v", event.NextSong)
	}
}

func TestHandleTrackStartedSkipsBanterNeededWhenUpcomingIsBanter(t *testing.T) {
	stateDir := t.TempDir()
	banterDir := filepath.Join(stateDir, "tts", "banter")
	if err := os.MkdirAll(banterDir, 0o755); err != nil {
		t.Fatalf("mkdir banter dir: %v", err)
	}

	currentPath := filepath.Join(stateDir, "current.opus")
	nextBanterPath := filepath.Join(banterDir, "line.aiff")

	playlist := []map[string]interface{}{
		{"filename": currentPath, "current": true, "playing": true},
		{"filename": nextBanterPath},
	}
	playlistRaw, _ := json.Marshal(playlist)

	fake := &fakeControllerMPVClient{props: map[string]json.RawMessage{
		"path":     mustRaw(currentPath),
		"playlist": playlistRaw,
	}}

	svc := &service{
		cfg:    &config.Config{Station: config.StationConfig{StateDir: stateDir, QueueDepth: 5}, TTS: config.TTSConfig{DataDir: filepath.Join(stateDir, "tts")}},
		st:     &Station{StateDir: stateDir},
		client: fake,
		events: NewAgentEventStore(stateDir),
	}

	if err := svc.handleTrackStarted(); err != nil {
		t.Fatalf("handleTrackStarted error: %v", err)
	}

	event, err := svc.events.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if event.Event != "timeout" {
		t.Fatalf("event=%q, want timeout (no actionable cue)", event.Event)
	}
}

func mustRaw(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
