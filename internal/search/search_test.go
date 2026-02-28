package search

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractAcceptanceCriteria(t *testing.T) {
	t.Run("wikitable fixture returns at least 80 percent expected pairs", func(t *testing.T) {
		html := mustReadFixture(t, "wikipedia_billboard_year_end_hot_100.html")

		got := Wikitable(html)
		want := []Result{
			{Artist: "Ke$ha", Title: "Tik Tok"},
			{Artist: "Lady Gaga", Title: "Poker Face"},
			{Artist: "Jay-Z featuring Alicia Keys", Title: "Empire State of Mind"},
			{Artist: "Train", Title: "Hey, Soul Sister"},
			{Artist: "The Black Eyed Peas", Title: "I Gotta Feeling"},
			{Artist: "Taio Cruz", Title: "Break Your Heart"},
			{Artist: "Rihanna", Title: "Rude Boy"},
			{Artist: "B.o.B featuring Hayley Williams", Title: "Airplanes"},
			{Artist: "Usher featuring will.i.am", Title: "OMG"},
			{Artist: "Eminem featuring Rihanna", Title: "Love the Way You Lie"},
		}

		matched := 0
		gotSet := make(map[string]struct{}, len(got))
		for _, r := range got {
			gotSet[resultKey(r)] = struct{}{}
		}
		for _, w := range want {
			if _, ok := gotSet[resultKey(w)]; ok {
				matched++
			}
		}

		ratio := float64(matched) / float64(len(want))
		if ratio < 0.8 {
			t.Fatalf("Wikitable() matched %.2f%% of expected rows (%d/%d), want >= 80%%", ratio*100, matched, len(want))
		}
	})

	t.Run("discogs fixture returns all tracks", func(t *testing.T) {
		html := mustReadFixture(t, "discogs_compilation_tracklist.html")
		got := Discogs(html)
		want := []Result{
			{Artist: "Madonna", Title: "Holiday"},
			{Artist: "Whitney Houston", Title: "I Wanna Dance with Somebody"},
			{Artist: "a-ha", Title: "Take On Me"},
			{Artist: "Pet Shop Boys", Title: "West End Girls"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Discogs() = %#v, want %#v", got, want)
		}
	})

	t.Run("musicbrainz fixture returns release artist with titles", func(t *testing.T) {
		html := mustReadFixture(t, "musicbrainz_release_page.html")
		got := MusicBrainz(html)
		want := []Result{
			{Artist: "Taylor Swift", Title: "Lavender Haze"},
			{Artist: "Taylor Swift", Title: "Maroon"},
			{Artist: "Taylor Swift", Title: "Anti-Hero"},
			{Artist: "Taylor Swift", Title: "Snow on the Beach"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("MusicBrainz() = %#v, want %#v", got, want)
		}
	})

	t.Run("generic hyphen format", func(t *testing.T) {
		got := Generic("The Beatles - Hey Jude")
		want := []Result{{Artist: "The Beatles", Title: "Hey Jude"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Generic() = %#v, want %#v", got, want)
		}
	})

	t.Run("generic by format", func(t *testing.T) {
		got := Generic("Shape of You by Ed Sheeran")
		want := []Result{{Artist: "Ed Sheeran", Title: "Shape of You"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Generic() = %#v, want %#v", got, want)
		}
	})

	t.Run("generic em dash format", func(t *testing.T) {
		got := Generic("The Beatles – Hey Jude")
		want := []Result{{Artist: "The Beatles", Title: "Hey Jude"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Generic() = %#v, want %#v", got, want)
		}
	})

	t.Run("generic noise is filtered", func(t *testing.T) {
		got := Generic("some song lyrics karaoke nightcore")
		if len(got) != 0 {
			t.Fatalf("Generic() = %#v, want empty result", got)
		}
	})

	t.Run("generic timestamp not treated as song", func(t *testing.T) {
		got := Generic("3:45 - 4:12")
		if len(got) != 0 {
			t.Fatalf("Generic() = %#v, want empty result", got)
		}
	})
}

func mustReadFixture(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(data)
}

func resultKey(r Result) string {
	return r.Artist + "||" + r.Title
}
