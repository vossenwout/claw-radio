package search

import "testing"

func TestRankCandidatesPrefersTrustedSourceAndDedupes(t *testing.T) {
	candidates := []Candidate{
		{Result: Result{Artist: "Kendrick Lamar", Title: "Not Like Us"}, Domain: "wikipedia.org", Parser: "wikipedia", SearchScore: 3.0},
		{Result: Result{Artist: "Kendrick Lamar", Title: "Not Like Us"}, Domain: "billboard.com", Parser: "billboard", SearchScore: 2.0},
		{Result: Result{Artist: "/* bad */", Title: "View all posts"}, Domain: "example.com", Parser: "generic", SearchScore: 4.0},
	}

	got := RankCandidates(candidates, 10)
	if len(got) != 1 {
		t.Fatalf("RankCandidates() len=%d, want 1 (%v)", len(got), got)
	}
	if got[0].Artist != "Kendrick Lamar" || got[0].Title != "Not Like Us" {
		t.Fatalf("RankCandidates()[0]=%#v, want Kendrick Lamar - Not Like Us", got[0])
	}
}

func TestRankCandidatesRespectsMaxResults(t *testing.T) {
	candidates := []Candidate{
		{Result: Result{Artist: "Artist 1", Title: "Song 1"}, Domain: "wikipedia.org", Parser: "wikipedia", SearchScore: 3.0},
		{Result: Result{Artist: "Artist 2", Title: "Song 2"}, Domain: "billboard.com", Parser: "billboard", SearchScore: 2.0},
		{Result: Result{Artist: "Artist 3", Title: "Song 3"}, Domain: "musicbrainz.org", Parser: "musicbrainz", SearchScore: 1.0},
	}

	got := RankCandidates(candidates, 2)
	if len(got) != 2 {
		t.Fatalf("RankCandidates() len=%d, want 2 (%v)", len(got), got)
	}
}

func TestRankCandidatesFiltersListHeadings(t *testing.T) {
	candidates := []Candidate{
		{Result: Result{Artist: "Women", Title: "Greatest of All Time Hot 100 Songs"}, Domain: "billboard.com", Parser: "generic", SearchScore: 4.0},
		{Result: Result{Artist: "Nirvana", Title: "Smells Like Teen Spirit"}, Domain: "wikipedia.org", Parser: "wikipedia", SearchScore: 2.0},
	}

	got := RankCandidates(candidates, 10)
	if len(got) != 1 {
		t.Fatalf("RankCandidates() len=%d, want 1 (%v)", len(got), got)
	}
	if got[0].Artist != "Nirvana" || got[0].Title != "Smells Like Teen Spirit" {
		t.Fatalf("RankCandidates()[0]=%#v, want Nirvana - Smells Like Teen Spirit", got[0])
	}
}
