package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
)

func TestPlaybackCommandsExitFiveWhenMPVNotRunning(t *testing.T) {
	cfg := &config.Config{
		MPV: config.MPVConfig{
			Socket: filepath.Join(t.TempDir(), "missing.sock"),
		},
		Station: config.StationConfig{
			CacheDir: t.TempDir(),
		},
	}

	resolver := &fakePlaybackResolver{path: "/tmp/resolved.opus"}
	restore := withPlaybackTestHooks(cfg, resolver)
	defer restore()

	tests := []struct {
		name string
		args []string
	}{
		{name: "play", args: []string{"play", "Daft Punk - Get Lucky"}},
		{name: "queue", args: []string{"queue", "https://example.com/song"}},
		{name: "pause", args: []string{"pause"}},
		{name: "resume", args: []string{"resume"}},
		{name: "next", args: []string{"next"}},
	}

	for _, tt := range tests {
		err := executeCommandForTest(t, tt.args...)
		assertExitCode(t, err, 5)
		if !strings.Contains(err.Error(), "claw-radio start") {
			t.Fatalf("%s error must suggest claw-radio start, got %q", tt.name, err)
		}
	}
}

func TestPlayCallsInsertNextThenPlaylistNext(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "play")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
		YtDlp: config.BinaryConfig{
			Binary: "/usr/bin/yt-dlp",
		},
		Station: config.StationConfig{
			CacheDir: t.TempDir(),
		},
	}
	resolver := &fakePlaybackResolver{path: "/tmp/get-lucky.opus"}
	restore := withPlaybackTestHooks(cfg, resolver)
	defer restore()

	if err := executeCommandForTest(t, "play", "Daft Punk - Get Lucky"); err != nil {
		t.Fatalf("play failed: %v", err)
	}

	mock.wait(t)
	if len(resolver.seeds) != 1 || resolver.seeds[0] != "Daft Punk - Get Lucky" {
		t.Fatalf("resolver called with %v, want [Daft Punk - Get Lucky]", resolver.seeds)
	}

	want := [][]interface{}{
		{"loadfile", "/tmp/get-lucky.opus", "append"},
		{"get_property", "playlist-pos"},
		{"get_property", "playlist-count"},
		{"playlist-move", float64(1), float64(1)},
		{"playlist-next"},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv command sequence:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

func TestQueueWithDirectURLResolvesURLAndAppends(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "queue-url")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
		Station: config.StationConfig{
			CacheDir: t.TempDir(),
		},
	}
	resolver := &fakePlaybackResolver{path: "/tmp/from-url.opus"}
	restore := withPlaybackTestHooks(cfg, resolver)
	defer restore()

	url := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	if err := executeCommandForTest(t, "queue", url); err != nil {
		t.Fatalf("queue failed: %v", err)
	}

	mock.wait(t)
	if len(resolver.seeds) != 1 || resolver.seeds[0] != url {
		t.Fatalf("resolver called with %v, want [%s]", resolver.seeds, url)
	}

	want := [][]interface{}{
		{"loadfile", "/tmp/from-url.opus", "append"},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv commands:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

func TestQueueWithQueryAppendsResolvedPath(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "queue-query")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
		Station: config.StationConfig{
			CacheDir: t.TempDir(),
		},
	}
	resolver := &fakePlaybackResolver{path: "/tmp/query-track.opus"}
	restore := withPlaybackTestHooks(cfg, resolver)
	defer restore()

	if err := executeCommandForTest(t, "queue", "Toto - Africa"); err != nil {
		t.Fatalf("queue failed: %v", err)
	}

	mock.wait(t)
	want := [][]interface{}{
		{"loadfile", "/tmp/query-track.opus", "append"},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv commands:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

func TestPauseSendsSetPropertyPauseTrue(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "pause")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
	}
	restore := withPlaybackTestHooks(cfg, &fakePlaybackResolver{})
	defer restore()

	if err := executeCommandForTest(t, "pause"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	mock.wait(t)
	want := [][]interface{}{
		{"set_property", "pause", true},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv commands:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

func TestResumeSendsSetPropertyPauseFalse(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "resume")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
	}
	restore := withPlaybackTestHooks(cfg, &fakePlaybackResolver{})
	defer restore()

	if err := executeCommandForTest(t, "resume"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	mock.wait(t)
	want := [][]interface{}{
		{"set_property", "pause", false},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv commands:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

func TestNextSendsPlaylistNext(t *testing.T) {
	socketPath := makePlaybackSocketPath(t, "next")
	mock := startPlaybackMPVServer(t, socketPath)

	cfg := &config.Config{
		MPV: config.MPVConfig{Socket: socketPath},
	}
	restore := withPlaybackTestHooks(cfg, &fakePlaybackResolver{})
	defer restore()

	if err := executeCommandForTest(t, "next"); err != nil {
		t.Fatalf("next failed: %v", err)
	}

	mock.wait(t)
	want := [][]interface{}{
		{"playlist-next"},
	}
	if !reflect.DeepEqual(mock.commands(), want) {
		t.Fatalf("unexpected mpv commands:\n got: %#v\nwant: %#v", mock.commands(), want)
	}
}

type fakePlaybackResolver struct {
	path  string
	err   error
	seeds []string
}

func (f *fakePlaybackResolver) Resolve(seed, _ string) (string, error) {
	f.seeds = append(f.seeds, seed)
	if f.err != nil {
		return "", f.err
	}
	return f.path, nil
}

func withPlaybackTestHooks(cfg *config.Config, resolver playbackResolver) func() {
	origLoad := loadConfigFn
	origDial := dialPlaybackClientFn
	origResolver := newPlaybackResolverFn

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	dialPlaybackClientFn = func(socketPath string) (playbackClient, error) {
		return mpv.Dial(socketPath)
	}
	newPlaybackResolverFn = func(string) playbackResolver {
		return resolver
	}

	return func() {
		loadConfigFn = origLoad
		dialPlaybackClientFn = origDial
		newPlaybackResolverFn = origResolver
	}
}

type playbackMPVServer struct {
	listener *net.UnixListener
	path     string

	mu   sync.Mutex
	cmds [][]interface{}

	done chan struct{}
	err  chan error
}

func startPlaybackMPVServer(t *testing.T, socketPath string) *playbackMPVServer {
	t.Helper()

	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &playbackMPVServer{
		listener: ln,
		path:     socketPath,
		done:     make(chan struct{}),
		err:      make(chan error, 1),
	}

	go server.serve()

	t.Cleanup(func() {
		_ = server.listener.Close()
		_ = os.Remove(server.path)
	})

	return server
}

func (s *playbackMPVServer) serve() {
	defer close(s.done)

	conn, err := s.listener.AcceptUnix()
	if err != nil {
		s.err <- err
		return
	}
	defer conn.Close()

	dec := jsonDecoder(conn)
	enc := jsonEncoder(conn)

	for {
		var req map[string]interface{}
		if err := dec(&req); err != nil {
			if errors.Is(err, io.EOF) {
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
		s.mu.Lock()
		s.cmds = append(s.cmds, cmd)
		s.mu.Unlock()

		resp := map[string]interface{}{
			"error":      "success",
			"request_id": req["request_id"],
		}
		if len(cmd) >= 2 && cmd[0] == "get_property" {
			if prop, _ := cmd[1].(string); prop == "playlist-pos" {
				resp["data"] = 0
			} else if prop == "playlist-count" {
				resp["data"] = 2
			}
		}
		if err := enc(resp); err != nil {
			s.err <- err
			return
		}
	}
}

func (s *playbackMPVServer) wait(t *testing.T) {
	t.Helper()

	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mpv mock server to finish")
	}

	select {
	case err := <-s.err:
		if !isExpectedAcceptError(err) {
			t.Fatalf("mpv mock server error: %v", err)
		}
	default:
	}
}

func (s *playbackMPVServer) commands() [][]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([][]interface{}, len(s.cmds))
	copy(out, s.cmds)
	return out
}

func makePlaybackSocketPath(t *testing.T, suffix string) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/claw-radio-cmd-%s-%d-%d.sock", suffix, os.Getpid(), time.Now().UnixNano())
	_ = os.Remove(path)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}

func isExpectedAcceptError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "use of closed network connection")
}

func jsonDecoder(conn net.Conn) func(v interface{}) error {
	dec := json.NewDecoder(conn)
	return dec.Decode
}

func jsonEncoder(conn net.Conn) func(v interface{}) error {
	enc := json.NewEncoder(conn)
	return enc.Encode
}
