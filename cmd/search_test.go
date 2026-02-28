package cmd

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestSearchCommandOutputsJSONAndStatsLine(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<p>Britney Spears - Oops! I Did It Again</p><p>NSYNC - Bye Bye Bye</p>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{
				{"url": page.URL + "/songs"},
			},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{
		Search: config.SearchConfig{SearxNGURL: searx.URL},
	}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("search", "test query")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}

	var got []string
	if decodeErr := json.Unmarshal([]byte(stdout), &got); decodeErr != nil {
		t.Fatalf("stdout is not valid JSON array: %v; stdout=%q", decodeErr, stdout)
	}

	want := []string{
		"Britney Spears - Oops! I Did It Again",
		"NSYNC - Bye Bye Bye",
	}
	if len(got) != len(want) {
		t.Fatalf("result len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result[%d] = %q, want %q", i, got[i], want[i])
		}
		if !strings.Contains(got[i], " - ") {
			t.Fatalf("result[%d] = %q, want Artist - Title format", i, got[i])
		}
	}

	if !strings.Contains(stderr, "Fetched 1 pages, extracted 2 unique songs.") {
		t.Fatalf("stderr missing fetch summary, got: %q", stderr)
	}
}

func TestSearchCommandSearxUnreachableExitsOne(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg := &config.Config{
		Search: config.SearchConfig{SearxNGURL: "http://" + addr},
	}
	restore := withSearchTestHooks(cfg)
	defer restore()

	execErr, _, stderr := executeCommandWithOutputForTest("search", "test query")
	assertExitCode(t, execErr, 1)
	if !strings.Contains(strings.ToLower(stderr), "searxng unreachable") {
		t.Fatalf("stderr = %q, want contains %q", stderr, "searxng unreachable")
	}
}

func TestSearchCommandWithoutQueryExitsTwo(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{SearxNGURL: "http://localhost:8888"},
	}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("search")
	assertExitCode(t, err, 2)
	if !strings.Contains(stdout+stderr, "Usage:") {
		t.Fatalf("usage output missing: stdout=%q stderr=%q", stdout, stderr)
	}
}

func withSearchTestHooks(cfg *config.Config) func() {
	origLoad := loadConfigFn
	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}

	return func() {
		loadConfigFn = origLoad
	}
}
