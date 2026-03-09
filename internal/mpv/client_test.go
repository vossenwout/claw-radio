package mpv

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDialReturnsErrorWhenSocketMissing(t *testing.T) {
	socketPath := makeSocketPath(t, "missing")

	_, err := Dial(socketPath)
	if err == nil {
		t.Fatalf("expected Dial() to fail for missing socket")
	}
}

func TestCommandSendsJSONAndReturnsNilOnSuccess(t *testing.T) {
	socketPath := makeSocketPath(t, "command")
	received := make(chan map[string]interface{}, 1)

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()

		var req map[string]interface{}
		mustDecode(t, conn, &req)
		received <- req

		resp := map[string]interface{}{
			"error":      "success",
			"request_id": req["request_id"],
		}
		mustEncode(t, conn, resp)
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	if err := client.Command("cycle", "pause"); err != nil {
		t.Fatalf("Command() error: %v", err)
	}

	req := <-received
	cmd, ok := req["command"].([]interface{})
	if !ok {
		t.Fatalf("request command is not an array: %#v", req["command"])
	}
	want := []interface{}{"cycle", "pause"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command payload: got %#v want %#v", cmd, want)
	}
}

func TestGetReturnsRawData(t *testing.T) {
	socketPath := makeSocketPath(t, "get")

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()

		var req map[string]interface{}
		mustDecode(t, conn, &req)
		resp := map[string]interface{}{
			"data":       30,
			"error":      "success",
			"request_id": req["request_id"],
		}
		mustEncode(t, conn, resp)
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	got, err := client.Get("volume")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if string(got) != "30" {
		t.Fatalf("unexpected raw data: got %q want %q", string(got), "30")
	}
}

func TestInsertNextSendsExpectedSequence(t *testing.T) {
	socketPath := makeSocketPath(t, "insertnext")
	received := make(chan [][]interface{}, 1)

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		cmds := make([][]interface{}, 0, 4)

		for i := 0; i < 4; i++ {
			var req map[string]interface{}
			if err := dec.Decode(&req); err != nil {
				t.Errorf("Decode() error: %v", err)
				return
			}
			reqID := req["request_id"]
			cmd, ok := req["command"].([]interface{})
			if !ok {
				t.Errorf("request command is not array: %#v", req["command"])
				return
			}
			cmds = append(cmds, cmd)

			resp := map[string]interface{}{
				"error":      "success",
				"request_id": reqID,
			}
			switch i {
			case 1:
				resp["data"] = 1
			case 2:
				resp["data"] = 3
			}

			if err := enc.Encode(resp); err != nil {
				t.Errorf("Encode() error: %v", err)
				return
			}
		}
		received <- cmds
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	if err := client.InsertNext("/tmp/song.mp3"); err != nil {
		t.Fatalf("InsertNext() error: %v", err)
	}

	got := <-received
	want := [][]interface{}{
		{"loadfile", "/tmp/song.mp3", "append"},
		{"get_property", "playlist-pos"},
		{"get_property", "playlist-count"},
		{"playlist-move", float64(2), float64(2)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command sequence:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestQueueNextUsesAppendPlayWhenIdle(t *testing.T) {
	socketPath := makeSocketPath(t, "queuenext-idle")
	received := make(chan [][]interface{}, 1)

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		cmds := make([][]interface{}, 0, 2)

		for i := 0; i < 2; i++ {
			var req map[string]interface{}
			if err := dec.Decode(&req); err != nil {
				t.Errorf("Decode() error: %v", err)
				return
			}
			reqID := req["request_id"]
			cmd, ok := req["command"].([]interface{})
			if !ok {
				t.Errorf("request command is not array: %#v", req["command"])
				return
			}
			cmds = append(cmds, cmd)

			resp := map[string]interface{}{
				"error":      "success",
				"request_id": reqID,
			}
			if i == 0 {
				resp["data"] = true
			}

			if err := enc.Encode(resp); err != nil {
				t.Errorf("Encode() error: %v", err)
				return
			}
		}
		received <- cmds
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	if err := client.QueueNext("/tmp/song.mp3"); err != nil {
		t.Fatalf("QueueNext() error: %v", err)
	}

	got := <-received
	want := [][]interface{}{
		{"get_property", "idle-active"},
		{"loadfile", "/tmp/song.mp3", "append-play"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command sequence:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestQueueNextFallsBackToInsertNextWhenActive(t *testing.T) {
	socketPath := makeSocketPath(t, "queuenext-active")
	received := make(chan [][]interface{}, 1)

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		cmds := make([][]interface{}, 0, 5)

		for i := 0; i < 5; i++ {
			var req map[string]interface{}
			if err := dec.Decode(&req); err != nil {
				t.Errorf("Decode() error: %v", err)
				return
			}
			reqID := req["request_id"]
			cmd, ok := req["command"].([]interface{})
			if !ok {
				t.Errorf("request command is not array: %#v", req["command"])
				return
			}
			cmds = append(cmds, cmd)

			resp := map[string]interface{}{
				"error":      "success",
				"request_id": reqID,
			}
			switch i {
			case 0:
				resp["data"] = false
			case 2:
				resp["data"] = 1
			case 3:
				resp["data"] = 3
			}

			if err := enc.Encode(resp); err != nil {
				t.Errorf("Encode() error: %v", err)
				return
			}
		}
		received <- cmds
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	if err := client.QueueNext("/tmp/song.mp3"); err != nil {
		t.Fatalf("QueueNext() error: %v", err)
	}

	got := <-received
	want := [][]interface{}{
		{"get_property", "idle-active"},
		{"loadfile", "/tmp/song.mp3", "append"},
		{"get_property", "playlist-pos"},
		{"get_property", "playlist-count"},
		{"playlist-move", float64(2), float64(2)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command sequence:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEventsContainsFileLoaded(t *testing.T) {
	socketPath := makeSocketPath(t, "events")

	ln := mustListenUnix(t, socketPath)
	defer closeListener(t, ln)

	go func() {
		conn := mustAcceptUnix(t, ln)
		defer conn.Close()
		mustEncode(t, conn, map[string]interface{}{"event": "file-loaded"})
	}()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	select {
	case event := <-client.Events():
		if event["event"] != "file-loaded" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestWaitForSocketReturnsNilWhenSocketAppears(t *testing.T) {
	socketPath := makeSocketPath(t, "wait-appears")
	done := make(chan struct{})

	go func() {
		time.Sleep(200 * time.Millisecond)
		ln := mustListenUnix(t, socketPath)
		defer closeListener(t, ln)

		conn := mustAcceptUnix(t, ln)
		conn.Close()
		close(done)
	}()

	if err := WaitForSocket(socketPath, time.Second); err != nil {
		t.Fatalf("WaitForSocket() error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive connection in time")
	}
}

func TestWaitForSocketReturnsTimeoutWhenSocketNeverAppears(t *testing.T) {
	socketPath := makeSocketPath(t, "wait-timeout")

	start := time.Now()
	err := WaitForSocket(socketPath, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
	if time.Since(start) < 180*time.Millisecond {
		t.Fatalf("wait duration too short: %v", time.Since(start))
	}
}

func mustListenUnix(t *testing.T, socketPath string) *net.UnixListener {
	t.Helper()

	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("ListenUnix() error: %v", err)
	}
	return ln
}

func closeListener(t *testing.T, ln *net.UnixListener) {
	t.Helper()
	if err := ln.Close(); err != nil && !isClosedErr(err) {
		t.Fatalf("Close() listener error: %v", err)
	}
}

func mustAcceptUnix(t *testing.T, ln *net.UnixListener) *net.UnixConn {
	t.Helper()
	conn, err := ln.AcceptUnix()
	if err != nil {
		t.Fatalf("AcceptUnix() error: %v", err)
	}
	return conn
}

func mustDecode(t *testing.T, conn net.Conn, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(conn).Decode(v); err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
}

func mustEncode(t *testing.T, conn net.Conn, v interface{}) {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(v); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
}

func isClosedErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "closed")
}

func makeSocketPath(t *testing.T, prefix string) string {
	t.Helper()

	path := fmt.Sprintf("/tmp/claw-radio-mpv-test-%s-%d-%d.sock", prefix, os.Getpid(), time.Now().UnixNano())
	_ = os.Remove(path)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}
