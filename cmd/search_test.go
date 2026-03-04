package cmd

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vossenwout/claw-radio/internal/config"
	searchpkg "github.com/vossenwout/claw-radio/internal/search"
)

func TestSearchCommandOutputsJSONAndStatsLine(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr><tr><td>Britney Spears</td><td>Oops! I Did It Again</td></tr><tr><td>NSYNC</td><td>Bye Bye Bye</td></tr></table>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{
				{"url": page.URL + "/wikipedia.org/songs"},
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

	if !strings.Contains(stderr, "Found 2 song candidates from 1 pages.") {
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
	if !strings.Contains(stderr, "could not reach SearxNG") {
		t.Fatalf("stderr = %q, want contains %q", stderr, "could not reach SearxNG")
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
	if !strings.Contains(stdout+stderr, "missing query") {
		t.Fatalf("missing query message not shown: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "Usage:") {
		t.Fatalf("usage output missing: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSearchCommandInvalidModeExitsTwo(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{SearxNGURL: "http://localhost:8888"},
	}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, _, stderr := executeCommandWithOutputForTest("search", "test query", "--mode", "nope")
	assertExitCode(t, err, 2)
	if !strings.Contains(stderr, "invalid mode") {
		t.Fatalf("stderr = %q, want invalid mode message", stderr)
	}
}

func TestSearchCommandDebugStatsPrinted(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<p>Britney Spears - Oops! I Did It Again</p>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{{"url": page.URL + "/songs"}},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{Search: config.SearchConfig{SearxNGURL: searx.URL}}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, _, stderr := executeCommandWithOutputForTest("search", "test query", "--debug")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}
	if !strings.Contains(stderr, "Queries:") {
		t.Fatalf("stderr missing debug queries line: %q", stderr)
	}
	if !strings.Contains(stderr, "Candidates before ranking:") {
		t.Fatalf("stderr missing debug candidate line: %q", stderr)
	}
}

func TestSearchCommandReportsUnresponsiveEnginesWhenNoHits(t *testing.T) {
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results":              []map[string]string{},
			"unresponsive_engines": [][]string{{"duckduckgo", "timeout"}},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{Search: config.SearchConfig{SearxNGURL: searx.URL}}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, stdout, stderr := executeCommandWithOutputForTest("search", "test query")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("stdout = %q, want empty JSON array", stdout)
	}
	if !strings.Contains(stderr, "Unresponsive engines") {
		t.Fatalf("stderr missing unresponsive engine note: %q", stderr)
	}
}

func TestSearchCommandUsesConfiguredModeEngineDefaults(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr><tr><td>Kendrick Lamar</td><td>Not Like Us</td></tr></table>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("engines"); got != "yahoo" {
			t.Fatalf("query engines = %q, want %q", got, "yahoo")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{{"url": page.URL + "/wikipedia.org/page"}},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{Search: config.SearchConfig{
		SearxNGURL: searx.URL,
		ModeEngines: config.SearchModeEngines{
			ArtistTop: []string{"yahoo"},
		},
	}}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, _, _ := executeCommandWithOutputForTest("search", "Kendrick Lamar", "--mode", "artist-top")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}
}

func TestSearchCommandEnginesFlagOverridesConfig(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr><tr><td>Kendrick Lamar</td><td>DNA.</td></tr></table>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("engines"); got != "bing,yahoo" {
			t.Fatalf("query engines = %q, want %q", got, "bing,yahoo")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{{"url": page.URL + "/wikipedia.org/page"}},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{Search: config.SearchConfig{
		SearxNGURL: searx.URL,
		ModeEngines: config.SearchModeEngines{
			Raw: []string{"yahoo"},
		},
	}}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, _, _ := executeCommandWithOutputForTest("search", "Kendrick Lamar songs", "--engines", "bing,yahoo")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}
}

func TestSearchCommandCombinesModesAndModeEngines(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr><tr><td>SZA</td><td>Saturn</td></tr></table>`))
	}))
	defer page.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("engines"); got != "yahoo,bing" {
			t.Fatalf("query engines = %q, want %q", got, "yahoo,bing")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]string{{"url": page.URL + "/wikipedia.org/page"}},
		})
	}))
	defer searx.Close()

	cfg := &config.Config{Search: config.SearchConfig{
		SearxNGURL: searx.URL,
		ModeEngines: config.SearchModeEngines{
			ChartYear: []string{"yahoo"},
			GenreTop:  []string{"bing"},
		},
	}}
	restore := withSearchTestHooks(cfg)
	defer restore()

	err, _, _ := executeCommandWithOutputForTest("search", "best pop songs", "--mode", "chart-year,genre-top")
	if err != nil {
		t.Fatalf("search command failed: %v", err)
	}
}

func withSearchTestHooks(cfg *config.Config) func() {
	origLoad := loadConfigFn
	origMode := searchModeFlag
	origDebug := searchDebugFlag
	origExpand := searchExpandSuggestionsFlag
	origMaxPages := searchMaxPagesFlag
	origEngines := searchEnginesFlag

	searchModeFlag = string(searchpkg.ModeRaw)
	searchDebugFlag = false
	searchExpandSuggestionsFlag = false
	searchMaxPagesFlag = 0
	searchEnginesFlag = nil

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}

	return func() {
		loadConfigFn = origLoad
		searchModeFlag = origMode
		searchDebugFlag = origDebug
		searchExpandSuggestionsFlag = origExpand
		searchMaxPagesFlag = origMaxPages
		searchEnginesFlag = origEngines
	}
}
