package station

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	agentEventsLogFileName      = "agent-events.jsonl"
	agentEventsCursorFileName   = "agent-events.offset"
	pendingBanterFileName       = "pending-banter.json"
	pendingIntroFileName        = "pending-intro.json"
	agentEventsPollInterval     = 150 * time.Millisecond
	defaultPollTimeout          = 30 * time.Second
	defaultBanterDeadlineMillis = 3000
)

type AgentEventStore struct {
	stateDir string
}

type AgentSong struct {
	Artist string `json:"artist,omitempty"`
	Title  string `json:"title,omitempty"`
	Path   string `json:"path,omitempty"`
}

type AgentEvent struct {
	Event      string     `json:"event"`
	EventID    string     `json:"event_id,omitempty"`
	TS         int64      `json:"ts"`
	Prompt     string     `json:"prompt,omitempty"`
	NextSong   *AgentSong `json:"next_song,omitempty"`
	DeadlineMS int        `json:"deadline_ms,omitempty"`
	Voice      string     `json:"voice,omitempty"`
	Count      int        `json:"count,omitempty"`
	Depth      int        `json:"depth,omitempty"`
}

type PendingBanter struct {
	EventID     string    `json:"event_id"`
	TS          int64     `json:"ts"`
	Prompt      string    `json:"prompt,omitempty"`
	NextSong    AgentSong `json:"next_song"`
	DeadlineMS  int       `json:"deadline_ms"`
	Fulfilled   bool      `json:"fulfilled"`
	FulfilledAt int64     `json:"fulfilled_at,omitempty"`
}

type PendingIntro struct {
	AudioPath string `json:"audio_path"`
	TS        int64  `json:"ts"`
}

func NewAgentEventStore(stateDir string) *AgentEventStore {
	return &AgentEventStore{stateDir: strings.TrimSpace(stateDir)}
}

func (s *AgentEventStore) Ensure() error {
	if strings.TrimSpace(s.stateDir) == "" {
		return fmt.Errorf("state directory is empty")
	}
	return os.MkdirAll(s.stateDir, 0o755)
}

func (s *AgentEventStore) ClearRuntimeState() error {
	if err := s.Ensure(); err != nil {
		return err
	}
	for _, p := range []string{s.eventsPath(), s.cursorPath(), s.pendingBanterPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *AgentEventStore) Append(event AgentEvent) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	if strings.TrimSpace(event.Event) == "" {
		return fmt.Errorf("event name is empty")
	}
	if event.TS == 0 {
		event.TS = time.Now().Unix()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

func (s *AgentEventStore) Next(timeout time.Duration) (AgentEvent, error) {
	if err := s.Ensure(); err != nil {
		return AgentEvent{}, err
	}
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}
	deadline := time.Now().Add(timeout)

	for {
		event, ok, err := s.nextImmediate()
		if err != nil {
			return AgentEvent{}, err
		}
		if ok {
			return event, nil
		}

		if time.Now().After(deadline) {
			return AgentEvent{Event: "timeout", TS: time.Now().Unix()}, nil
		}
		time.Sleep(agentEventsPollInterval)
	}
}

func (s *AgentEventStore) nextImmediate() (AgentEvent, bool, error) {
	offset, err := s.readOffset()
	if err != nil {
		return AgentEvent{}, false, err
	}

	f, err := os.OpenFile(s.eventsPath(), os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return AgentEvent{}, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return AgentEvent{}, false, err
	}
	if offset >= info.Size() {
		return AgentEvent{}, false, nil
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return AgentEvent{}, false, err
	}

	r := bufio.NewReader(f)
	line, err := r.ReadString('\n')
	if err != nil {
		return AgentEvent{}, false, nil
	}
	nextOffset := offset + int64(len(line))
	if err := s.writeOffset(nextOffset); err != nil {
		return AgentEvent{}, false, err
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return AgentEvent{}, false, nil
	}

	var event AgentEvent
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return AgentEvent{}, false, nil
	}
	if event.TS == 0 {
		event.TS = time.Now().Unix()
	}
	return event, true, nil
}

func (s *AgentEventStore) LoadPendingBanter() (*PendingBanter, error) {
	data, err := os.ReadFile(s.pendingBanterPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pending PendingBanter
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func (s *AgentEventStore) SavePendingBanter(pending PendingBanter) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	if pending.DeadlineMS <= 0 {
		pending.DeadlineMS = defaultBanterDeadlineMillis
	}
	if pending.TS == 0 {
		pending.TS = time.Now().Unix()
	}
	return writeJSONAtomic(s.pendingBanterPath(), pending)
}

func (s *AgentEventStore) ClearPendingBanter() error {
	err := os.Remove(s.pendingBanterPath())
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *AgentEventStore) LoadPendingIntro() (*PendingIntro, error) {
	data, err := os.ReadFile(s.pendingIntroPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pending PendingIntro
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func (s *AgentEventStore) SavePendingIntro(audioPath string) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	pending := PendingIntro{AudioPath: strings.TrimSpace(audioPath), TS: time.Now().Unix()}
	if pending.AudioPath == "" {
		return fmt.Errorf("audio path is empty")
	}
	return writeJSONAtomic(s.pendingIntroPath(), pending)
}

func (s *AgentEventStore) ClearPendingIntro() error {
	err := os.Remove(s.pendingIntroPath())
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *AgentEventStore) readOffset() (int64, error) {
	data, err := os.ReadFile(s.cursorPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, nil
	}
	var offset int64
	if _, err := fmt.Sscanf(trimmed, "%d", &offset); err != nil {
		return 0, nil
	}
	if offset < 0 {
		return 0, nil
	}
	return offset, nil
}

func (s *AgentEventStore) writeOffset(offset int64) error {
	if offset < 0 {
		offset = 0
	}
	return os.WriteFile(s.cursorPath(), []byte(fmt.Sprintf("%d\n", offset)), 0o644)
}

func (s *AgentEventStore) eventsPath() string {
	return filepath.Join(s.stateDir, agentEventsLogFileName)
}

func (s *AgentEventStore) cursorPath() string {
	return filepath.Join(s.stateDir, agentEventsCursorFileName)
}

func (s *AgentEventStore) pendingBanterPath() string {
	return filepath.Join(s.stateDir, pendingBanterFileName)
}

func (s *AgentEventStore) pendingIntroPath() string {
	return filepath.Join(s.stateDir, pendingIntroFileName)
}
