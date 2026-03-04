package station

import (
	"testing"
	"time"
)

func TestAgentEventStoreAppendAndNext(t *testing.T) {
	store := NewAgentEventStore(t.TempDir())
	if err := store.Append(AgentEvent{Event: "queue_low", Count: 1, Depth: 5}); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	event, err := store.Next(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if event.Event != "queue_low" {
		t.Fatalf("event.Event=%q, want %q", event.Event, "queue_low")
	}
}

func TestAgentEventStoreNextTimeout(t *testing.T) {
	store := NewAgentEventStore(t.TempDir())
	event, err := store.Next(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if event.Event != "timeout" {
		t.Fatalf("event.Event=%q, want %q", event.Event, "timeout")
	}
}

func TestAgentEventStorePendingBanterRoundTrip(t *testing.T) {
	store := NewAgentEventStore(t.TempDir())
	pending := PendingBanter{
		EventID: "evt_123",
		NextSong: AgentSong{
			Artist: "Kendrick Lamar",
			Title:  "Money Trees",
			Path:   "/tmp/song.opus",
		},
	}
	if err := store.SavePendingBanter(pending); err != nil {
		t.Fatalf("SavePendingBanter() error: %v", err)
	}

	loaded, err := store.LoadPendingBanter()
	if err != nil {
		t.Fatalf("LoadPendingBanter() error: %v", err)
	}
	if loaded == nil || loaded.EventID != "evt_123" {
		t.Fatalf("loaded pending mismatch: %#v", loaded)
	}

	if err := store.ClearPendingBanter(); err != nil {
		t.Fatalf("ClearPendingBanter() error: %v", err)
	}
	loaded, err = store.LoadPendingBanter()
	if err != nil {
		t.Fatalf("LoadPendingBanter() after clear error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil pending after clear, got %#v", loaded)
	}
}
