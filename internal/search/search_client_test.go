package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestFetchURLsReturnsTopResultURLs(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Artist A - Song A</body></html>"))
	}))
	defer resultServer.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("q"); got != "test" {
			t.Fatalf("query q = %q, want %q", got, "test")
		}
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Fatalf("query format = %q, want %q", got, "json")
		}

		writeJSON(w, map[string]interface{}{
			"results": []map[string]string{
				{"url": resultServer.URL + "/1"},
				{"url": resultServer.URL + "/2"},
				{"url": resultServer.URL + "/3"},
			},
		})
	}))
	defer searx.Close()

	client := NewClient(searx.URL)
	urls, err := client.fetchURLs("test", 6)
	if err != nil {
		t.Fatalf("fetchURLs() error: %v", err)
	}

	want := []string{resultServer.URL + "/1", resultServer.URL + "/2", resultServer.URL + "/3"}
	if len(urls) != len(want) {
		t.Fatalf("fetchURLs() len = %d, want %d (%v)", len(urls), len(want), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("fetchURLs()[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestSearchDeduplicatesAndCapsMaxResults(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/wikipedia.org/page":
			_, _ = w.Write([]byte(buildWikitableHTML(50)))
		case "/discogs.com/page":
			_, _ = w.Write([]byte(buildDiscogsHTMLWithOneDuplicate()))
		default:
			http.NotFound(w, r)
		}
	}))
	defer resultServer.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"results": []map[string]string{
				{"url": resultServer.URL + "/wikipedia.org/page"},
				{"url": resultServer.URL + "/discogs.com/page"},
			},
		})
	}))
	defer searx.Close()

	client := NewClient(searx.URL)
	results, err := client.Search("test", 55)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	if len(results) != 55 {
		t.Fatalf("Search() len = %d, want 55", len(results))
	}

	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		k := strings.ToLower(r.Artist + "||" + r.Title)
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate result returned: %q", k)
		}
		seen[k] = struct{}{}
	}
}

func TestSearchDeduplicatesSamePairAcrossPages(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>The Beatles - Hey Jude</p></body></html>"))
	}))
	defer resultServer.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"results": []map[string]string{
				{"url": resultServer.URL + "/page1"},
				{"url": resultServer.URL + "/page2"},
			},
		})
	}))
	defer searx.Close()

	client := NewClient(searx.URL)
	results, err := client.Search("duplicate", 150)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	want := []Result{{Artist: "The Beatles", Title: "Hey Jude"}}
	if len(results) != len(want) {
		t.Fatalf("Search() len = %d, want %d (%v)", len(results), len(want), results)
	}
	if results[0] != want[0] {
		t.Fatalf("Search()[0] = %#v, want %#v", results[0], want[0])
	}
}

func TestSearchReturnsSearxUnreachableOnHTTP500(t *testing.T) {
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer searx.Close()

	client := NewClient(searx.URL)
	_, err := client.Search("test", 10)
	if err == nil {
		t.Fatal("Search() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "searxng unreachable") {
		t.Fatalf("Search() error = %q, want contains %q", err.Error(), "searxng unreachable")
	}
}

func TestSearchSkips404ResultPages(t *testing.T) {
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("<html><body>ABBA - Dancing Queen</body></html>"))
		case "/missing":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer resultServer.Close()

	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"results": []map[string]string{
				{"url": resultServer.URL + "/ok"},
				{"url": resultServer.URL + "/missing"},
			},
		})
	}))
	defer searx.Close()

	client := NewClient(searx.URL)
	results, err := client.Search("test", 150)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	want := []Result{{Artist: "ABBA", Title: "Dancing Queen"}}
	if len(results) != len(want) {
		t.Fatalf("Search() len = %d, want %d (%v)", len(results), len(want), results)
	}
	if results[0] != want[0] {
		t.Fatalf("Search()[0] = %#v, want %#v", results[0], want[0])
	}
}

func TestFetchAndExtractDispatchByURLContainsDomain(t *testing.T) {
	htmlByPath := map[string]string{
		"/wikipedia.org/source":   `<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr><tr><td>Wiki Artist</td><td>Wiki Song</td></tr></table>`,
		"/discogs.com/source":     `<meta property="og:title" content="Fallback Artist – Album"><span class="tracklist_track_artists">Discogs Artist</span><span class="tracklist_track_title">Discogs Song</span>`,
		"/musicbrainz.org/source": `<meta property="og:title" content="MB Artist – Release"><table><tr><td class="title">MB Song</td></tr></table>`,
		"/other/source":           `<p>Generic Artist - Generic Song</p>`,
	}

	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := htmlByPath[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer resultServer.Close()

	client := NewClient("http://unused")

	tests := []struct {
		name string
		url  string
		want []Result
	}{
		{name: "wikipedia", url: resultServer.URL + "/wikipedia.org/source", want: []Result{{Artist: "Wiki Artist", Title: "Wiki Song"}}},
		{name: "discogs", url: resultServer.URL + "/discogs.com/source", want: []Result{{Artist: "Discogs Artist", Title: "Discogs Song"}}},
		{name: "musicbrainz", url: resultServer.URL + "/musicbrainz.org/source", want: []Result{{Artist: "MB Artist", Title: "MB Song"}}},
		{name: "generic", url: resultServer.URL + "/other/source", want: []Result{{Artist: "Generic Artist", Title: "Generic Song"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.fetchAndExtract(tc.url)
			if err != nil {
				t.Fatalf("fetchAndExtract() error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("fetchAndExtract() len = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			if got[0] != tc.want[0] {
				t.Fatalf("fetchAndExtract()[0] = %#v, want %#v", got[0], tc.want[0])
			}
		})
	}
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func buildWikitableHTML(n int) string {
	var b strings.Builder
	b.WriteString(`<table class="wikitable"><tr><th>Artist</th><th>Title</th></tr>`)
	for i := 1; i <= n; i++ {
		b.WriteString(fmt.Sprintf(`<tr><td>Artist %d</td><td>Song %d</td></tr>`, i, i))
	}
	b.WriteString(`</table>`)
	return b.String()
}

func buildDiscogsHTMLWithOneDuplicate() string {
	type pair struct {
		artist string
		title  string
	}
	pairs := []pair{{artist: "Artist 10", title: "Song 10"}}
	for i := 51; i <= 59; i++ {
		pairs = append(pairs, pair{artist: fmt.Sprintf("Artist %d", i), title: fmt.Sprintf("Song %d", i)})
	}

	sort.SliceStable(pairs, func(i, j int) bool { return i < j })

	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(fmt.Sprintf(`<span class="tracklist_track_artists">%s</span>`, p.artist))
		b.WriteString(fmt.Sprintf(`<span class="tracklist_track_title">%s</span>`, p.title))
	}
	return b.String()
}
