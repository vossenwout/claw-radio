package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

func TestEventsWhenMPVNotRunningExitsFive(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	err := executeCommandForTest(t, "events")
	assertExitCode(t, err, 5)
}

func TestEventsJSONEmitsTrackStartedWithTitleAndDuration(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "events-track-started")
	server := startEventsCommandMPVServer(t, socketPath, "Britney Spears - Oops! I Did It Again", 211.0, 3)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		Station: config.StationConfig{
			QueueDepth: 5,
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	go func() {
		server.emit(t, map[string]interface{}{"event": "file-loaded"})
		time.Sleep(40 * time.Millisecond)
		server.closeConn()
	}()

	err, stdout, _ := executeCommandWithOutputForTest("events", "--json")
	if err != nil {
		t.Fatalf("events --json failed: %v", err)
	}
	server.wait(t)

	events := decodeJSONEventsFromOutput(t, stdout)
	started := findEventByType(events, "track_started")
	if started == nil {
		t.Fatalf("track_started event missing in output: %q", stdout)
	}

	if got, _ := started["title"].(string); got != "Britney Spears - Oops! I Did It Again" {
		t.Fatalf("track_started title = %q, want %q", got, "Britney Spears - Oops! I Did It Again")
	}
	if got := asFloat(started["duration"]); got != 211.0 {
		t.Fatalf("track_started duration = %v, want %v", got, 211.0)
	}
	if _, ok := started["ts"]; !ok {
		t.Fatalf("track_started missing ts field: %#v", started)
	}
}

func TestEventsJSONEmitsTrackEndedAndQueueLow(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "events-end-file")
	server := startEventsCommandMPVServer(t, socketPath, "Donna Summer - I Feel Love", 350.0, 1)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
		Station: config.StationConfig{
			QueueDepth: 5,
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	go func() {
		server.emit(t, map[string]interface{}{"event": "file-loaded"})
		server.emit(t, map[string]interface{}{"event": "end-file"})
		time.Sleep(40 * time.Millisecond)
		server.closeConn()
	}()

	err, stdout, _ := executeCommandWithOutputForTest("events", "--json")
	if err != nil {
		t.Fatalf("events --json failed: %v", err)
	}
	server.wait(t)

	events := decodeJSONEventsFromOutput(t, stdout)

	ended := findEventByType(events, "track_ended")
	if ended == nil {
		t.Fatalf("track_ended event missing in output: %q", stdout)
	}
	if got, _ := ended["title"].(string); got != "Donna Summer - I Feel Love" {
		t.Fatalf("track_ended title = %q, want %q", got, "Donna Summer - I Feel Love")
	}

	queueLow := findEventByType(events, "queue_low")
	if queueLow == nil {
		t.Fatalf("queue_low event missing in output: %q", stdout)
	}
	if got := asInt(queueLow["count"]); got != 1 {
		t.Fatalf("queue_low count = %d, want 1", got)
	}
	if got := asInt(queueLow["depth"]); got != 5 {
		t.Fatalf("queue_low depth = %d, want 5", got)
	}
}

func TestEventsJSONEmitsEngineStoppedOnSocketClose(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "events-engine-stopped")
	server := startEventsCommandMPVServer(t, socketPath, "Any Song", 200.0, 3)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	go func() {
		time.Sleep(30 * time.Millisecond)
		server.closeConn()
	}()

	err, stdout, _ := executeCommandWithOutputForTest("events", "--json")
	if err != nil {
		t.Fatalf("events --json failed: %v", err)
	}
	server.wait(t)

	events := decodeJSONEventsFromOutput(t, stdout)
	if findEventByType(events, "engine_stopped") == nil {
		t.Fatalf("engine_stopped event missing in output: %q", stdout)
	}
}

func TestEventsWithoutJSONIsHumanReadable(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "events-human")
	server := startEventsCommandMPVServer(t, socketPath, "A-Ha - Take On Me", 225.0, 3)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	go func() {
		server.emit(t, map[string]interface{}{"event": "file-loaded"})
		time.Sleep(40 * time.Millisecond)
		server.closeConn()
	}()

	err, stdout, _ := executeCommandWithOutputForTest("events")
	if err != nil {
		t.Fatalf("events failed: %v", err)
	}
	server.wait(t)

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	foundTrackStarted := false
	for _, line := range lines {
		if strings.Contains(line, "track_started") {
			foundTrackStarted = true
			if strings.Contains(line, "{") || strings.Contains(line, "}") {
				t.Fatalf("track_started line should be human-readable, got %q", line)
			}
			break
		}
	}
	if !foundTrackStarted {
		t.Fatalf("track_started line missing in output: %q", stdout)
	}
}

func TestEventsJSONFlushesEachLineImmediately(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "events-flush")
	server := startEventsCommandMPVServer(t, socketPath, "The Weeknd - Blinding Lights", 200.0, 3)

	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: socketPath,
		},
	}
	restore := withEventsTestHooks(cfg)
	defer restore()

	stdoutR, stdoutW := io.Pipe()
	defer stdoutR.Close()
	stderr := &bytes.Buffer{}

	RootCmd.SetOut(stdoutW)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs([]string{"events", "--json"})

	execErrCh := make(chan error, 1)
	go func() {
		execErrCh <- Execute()
		_ = stdoutW.Close()
	}()

	server.emit(t, map[string]interface{}{"event": "file-loaded"})

	lineCh := make(chan string, 1)
	readErrCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(stdoutR)
		sentFirst := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				readErrCh <- err
				return
			}
			if !sentFirst {
				lineCh <- line
				sentFirst = true
			}
		}
	}()

	select {
	case line := <-lineCh:
		if !strings.Contains(line, "\"event\":\"track_started\"") {
			t.Fatalf("first flushed line = %q, want track_started json", line)
		}
	case err := <-readErrCh:
		t.Fatalf("failed to read first json event line: %v", err)
	case err := <-execErrCh:
		t.Fatalf("events exited before first line flush: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for first json line to flush")
	}

	server.closeConn()

	select {
	case err := <-execErrCh:
		if err != nil {
			t.Fatalf("events exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events command to exit")
	}
	server.wait(t)
}

func withEventsTestHooks(cfg *config.Config) func() {
	origLoad := loadConfigFn
	origDial := dialEventsMPVClientFn
	origJSONFlag := eventsJSONFlag

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	dialEventsMPVClientFn = func(socketPath string) (eventsMPVClient, error) {
		return mpv.Dial(socketPath)
	}
	eventsJSONFlag = false

	return func() {
		loadConfigFn = origLoad
		dialEventsMPVClientFn = origDial
		eventsJSONFlag = origJSONFlag
	}
}

type eventsCommandMPVServer struct {
	listener *net.UnixListener
	path     string

	mu            sync.Mutex
	conn          *net.UnixConn
	enc           *json.Encoder
	mediaTitle    string
	duration      float64
	playlistCount int

	ready chan struct{}
	done  chan struct{}
	err   chan error
}

func startEventsCommandMPVServer(t *testing.T, socketPath, mediaTitle string, duration float64, playlistCount int) *eventsCommandMPVServer {
	t.Helper()

	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &eventsCommandMPVServer{
		listener:      ln,
		path:          socketPath,
		mediaTitle:    mediaTitle,
		duration:      duration,
		playlistCount: playlistCount,
		ready:         make(chan struct{}),
		done:          make(chan struct{}),
		err:           make(chan error, 1),
	}

	go server.serve()

	t.Cleanup(func() {
		_ = server.listener.Close()
		server.closeConn()
		_ = os.Remove(server.path)
	})

	return server
}

func (s *eventsCommandMPVServer) serve() {
	defer close(s.done)

	conn, err := s.listener.AcceptUnix()
	if err != nil {
		s.err <- err
		return
	}

	s.mu.Lock()
	s.conn = conn
	s.enc = json.NewEncoder(conn)
	s.mu.Unlock()
	close(s.ready)

	dec := json.NewDecoder(conn)
	for {
		var req map[string]interface{}
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || isExpectedAcceptError(err) {
				return
			}
			s.err <- err
			return
		}

		cmd, ok := req["command"].([]interface{})
		if !ok {
			s.err <- fmt.Errorf("request command was not []interface{}: %T", req["command"])
			return
		}

		resp := map[string]interface{}{
			"error":      "success",
			"request_id": req["request_id"],
		}

		if len(cmd) >= 2 && cmd[0] == "get_property" {
			prop, _ := cmd[1].(string)
			switch prop {
			case "media-title":
				resp["data"] = s.mediaTitle
			case "duration":
				resp["data"] = s.duration
			case "playlist-count":
				resp["data"] = s.playlistCount
			}
		}

		if err := s.write(resp); err != nil {
			if isExpectedAcceptError(err) {
				return
			}
			s.err <- err
			return
		}
	}
}

func (s *eventsCommandMPVServer) emit(t *testing.T, event map[string]interface{}) {
	t.Helper()
	s.waitReady(t)
	if err := s.write(event); err != nil && !isExpectedAcceptError(err) {
		t.Fatalf("emit event: %v", err)
	}
}

func (s *eventsCommandMPVServer) closeConn() {
	select {
	case <-s.ready:
	case <-time.After(2 * time.Second):
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

func (s *eventsCommandMPVServer) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-s.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events mpv mock connection")
	}
}

func (s *eventsCommandMPVServer) write(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enc == nil {
		return fmt.Errorf("encoder not ready")
	}
	return s.enc.Encode(v)
}

func (s *eventsCommandMPVServer) wait(t *testing.T) {
	t.Helper()

	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events mpv mock server to finish")
	}

	select {
	case err := <-s.err:
		if !isExpectedAcceptError(err) {
			t.Fatalf("events mpv mock server error: %v", err)
		}
	default:
	}
}

func decodeJSONEventsFromOutput(t *testing.T, stdout string) []map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	events := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func findEventByType(events []map[string]interface{}, eventType string) map[string]interface{} {
	for _, event := range events {
		if event["event"] == eventType {
			return event
		}
	}
	return nil
}
