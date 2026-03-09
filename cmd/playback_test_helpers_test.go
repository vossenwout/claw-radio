package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type playbackMPVServer struct {
	listener   *net.UnixListener
	path       string
	idleActive bool

	mu   sync.Mutex
	cmds [][]interface{}

	done chan struct{}
	err  chan error
}

func startPlaybackMPVServer(t *testing.T, socketPath string) *playbackMPVServer {
	return startPlaybackMPVServerWithIdleState(t, socketPath, false)
}

func startPlaybackMPVServerWithIdleState(t *testing.T, socketPath string, idleActive bool) *playbackMPVServer {
	t.Helper()

	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &playbackMPVServer{
		listener:   ln,
		path:       socketPath,
		idleActive: idleActive,
		done:       make(chan struct{}),
		err:        make(chan error, 1),
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
			} else if prop == "idle-active" {
				resp["data"] = s.idleActive
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
