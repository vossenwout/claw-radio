package station

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
)

type fakeQueueLowMPVClient struct {
	count int
	props map[string]json.RawMessage
}

func (f *fakeQueueLowMPVClient) Close() error                     { return nil }
func (f *fakeQueueLowMPVClient) PlaylistCount() (int, error)      { return f.count, nil }
func (f *fakeQueueLowMPVClient) PlaylistPaths() ([]string, error) { return nil, nil }
func (f *fakeQueueLowMPVClient) LoadFile(path, mode string) error { return nil }
func (f *fakeQueueLowMPVClient) Get(prop string) (json.RawMessage, error) {
	if raw, ok := f.props[prop]; ok {
		return raw, nil
	}
	return nil, errors.New("property not found")
}
func (f *fakeQueueLowMPVClient) Events() <-chan map[string]interface{} {
	return make(chan map[string]interface{})
}

func TestQueueLowCueArmsAndRearms(t *testing.T) {
	stateDir := t.TempDir()
	client := &fakeQueueLowMPVClient{
		count: 3,
		props: map[string]json.RawMessage{
			"playlist": mustRaw([]map[string]interface{}{
				{"filename": "played.opus"},
				{"filename": "current.opus", "current": true, "playing": true},
				{"filename": "next.opus"},
			}),
		},
	}
	store := NewAgentEventStore(stateDir)
	svc := &service{
		cfg:           &config.Config{Station: config.StationConfig{StateDir: stateDir, QueueDepth: 5}},
		st:            &Station{StateDir: stateDir},
		client:        client,
		events:        store,
		queueLowArmed: true,
	}

	svc.emitQueueLowIfNeeded()
	event, err := store.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("first queue_low poll failed: %v", err)
	}
	if event.Event != "queue_low" {
		t.Fatalf("event=%q, want queue_low", event.Event)
	}
	if event.Count != 1 {
		t.Fatalf("count=%d, want 1 upcoming song", event.Count)
	}

	svc.emitQueueLowIfNeeded()
	event, err = store.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("second poll failed: %v", err)
	}
	if event.Event != "timeout" {
		t.Fatalf("event=%q, want timeout (no duplicate queue_low)", event.Event)
	}

	client.count = 2
	client.props["playlist"] = mustRaw([]map[string]interface{}{
		{"filename": "played.opus"},
		{"filename": "current.opus", "current": true, "playing": true},
	})
	svc.emitQueueLowIfNeeded()
	event, err = store.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("third poll failed: %v", err)
	}
	if event.Event != "queue_low" {
		t.Fatalf("event=%q, want queue_low when upcoming reaches zero", event.Event)
	}
	if event.Count != 0 {
		t.Fatalf("count=%d, want 0 upcoming songs", event.Count)
	}

	client.count = 4
	client.props["playlist"] = mustRaw([]map[string]interface{}{
		{"filename": "played.opus"},
		{"filename": "current.opus", "current": true, "playing": true},
		{"filename": "next.opus"},
		{"filename": "later.opus"},
	})
	svc.emitQueueLowIfNeeded()

	client.count = 3
	client.props["playlist"] = mustRaw([]map[string]interface{}{
		{"filename": "played.opus"},
		{"filename": "current.opus", "current": true, "playing": true},
		{"filename": "next.opus"},
	})
	svc.emitQueueLowIfNeeded()
	event, err = store.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("fourth poll failed: %v", err)
	}
	if event.Event != "queue_low" {
		t.Fatalf("event=%q, want queue_low after rearm", event.Event)
	}
	if event.Count != 1 {
		t.Fatalf("count=%d, want 1 upcoming song after rearm", event.Count)
	}
}
