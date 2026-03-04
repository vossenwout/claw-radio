package search

import "testing"

func TestBuildProfileRawMode(t *testing.T) {
	got := BuildProfile(ModeRaw, "Kendrick Lamar songs")
	if len(got) != 1 || got[0] != "Kendrick Lamar songs" {
		t.Fatalf("BuildProfile(raw) = %#v, want single raw query", got)
	}
}

func TestBuildProfileArtistTopMode(t *testing.T) {
	got := BuildProfile(ModeArtistTop, "Kendrick Lamar songs")
	want := []string{
		"Kendrick Lamar most popular songs",
		"Kendrick Lamar top songs billboard",
		"Kendrick Lamar songs wikipedia",
		"Kendrick Lamar spotify top songs kworb",
	}

	if len(got) != len(want) {
		t.Fatalf("BuildProfile(artist-top) len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildProfile(artist-top)[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildProfileArtistYearMode(t *testing.T) {
	got := BuildProfile(ModeArtistYear, "kendrick lamar 2012")
	want := []string{
		"kendrick lamar most popular songs",
		"kendrick lamar top songs billboard",
		"kendrick lamar songs wikipedia",
		"kendrick lamar spotify top songs kworb",
		"kendrick lamar 2012 songs",
		"kendrick lamar songs released in 2012",
		"kendrick lamar 2012 tracklist",
		"kendrick lamar discography 2012",
		"kendrick lamar 2012 site:wikipedia.org",
	}
	if len(got) != len(want) {
		t.Fatalf("BuildProfile(artist-year) len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildProfile(artist-year)[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildProfileChartYearMode(t *testing.T) {
	got := BuildProfile(ModeChartYear, "Billboard Year-End Hot 100 2024")
	if len(got) != 4 {
		t.Fatalf("BuildProfile(chart-year) len=%d, want 4 (%v)", len(got), got)
	}
}

func TestBuildProfileChartYearModeNonChartQueryFallsBack(t *testing.T) {
	got := BuildProfile(ModeChartYear, "kendrick lamar 2012")
	want := []string{
		"kendrick lamar most popular songs",
		"kendrick lamar top songs billboard",
		"kendrick lamar songs wikipedia",
		"kendrick lamar spotify top songs kworb",
		"kendrick lamar 2012 songs",
		"kendrick lamar songs released in 2012",
		"kendrick lamar 2012 tracklist",
		"kendrick lamar discography 2012",
		"kendrick lamar 2012 site:wikipedia.org",
	}
	if len(got) != len(want) {
		t.Fatalf("BuildProfile(chart-year non-chart) len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildProfile(chart-year non-chart)[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildProfileGenreTopMode(t *testing.T) {
	got := BuildProfile(ModeGenreTop, "house music classics")
	if len(got) != 5 {
		t.Fatalf("BuildProfile(genre-top) len=%d, want 5 (%v)", len(got), got)
	}
}

func TestParseMode(t *testing.T) {
	if mode := ParseMode("raw"); mode != ModeRaw {
		t.Fatalf("ParseMode(raw)=%q, want %q", mode, ModeRaw)
	}
	if mode := ParseMode("artist-top"); mode != ModeArtistTop {
		t.Fatalf("ParseMode(artist-top)=%q, want %q", mode, ModeArtistTop)
	}
	if mode := ParseMode("artist-year"); mode != ModeArtistYear {
		t.Fatalf("ParseMode(artist-year)=%q, want %q", mode, ModeArtistYear)
	}
	if mode := ParseMode("chart-year"); mode != ModeChartYear {
		t.Fatalf("ParseMode(chart-year)=%q, want %q", mode, ModeChartYear)
	}
	if mode := ParseMode("genre-top"); mode != ModeGenreTop {
		t.Fatalf("ParseMode(genre-top)=%q, want %q", mode, ModeGenreTop)
	}
	if mode := ParseMode("unknown"); mode != "" {
		t.Fatalf("ParseMode(unknown)=%q, want empty", mode)
	}
}

func TestLooksLikeChartIntent(t *testing.T) {
	if looksLikeChartIntent("kendrick lamar 2012") {
		t.Fatalf("looksLikeChartIntent returned true for non-chart query")
	}
	if !looksLikeChartIntent("Billboard Year-End Hot 100 2012") {
		t.Fatalf("looksLikeChartIntent returned false for chart query")
	}
}

func TestExpandWithSuggestions(t *testing.T) {
	suggestions := []string{
		"Highest rated kendrick lamar songs",
		"Completely unrelated query",
	}
	got := ExpandWithSuggestions("kendrick lamager songs", suggestions, 1)
	if len(got) != 1 {
		t.Fatalf("ExpandWithSuggestions() len=%d, want 1 (%v)", len(got), got)
	}
	if got[0] != "Highest rated kendrick lamar songs" {
		t.Fatalf("ExpandWithSuggestions()[0]=%q, want %q", got[0], "Highest rated kendrick lamar songs")
	}
}

func TestNormalizeEngineList(t *testing.T) {
	got := NormalizeEngineList([]string{"yahoo", " bing,duckduckgo ", "Yahoo"})
	want := []string{"yahoo", "bing", "duckduckgo"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeEngineList() len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeEngineList()[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}
