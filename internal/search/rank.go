package search

import (
	"math"
	"sort"
	"strings"
)

type Candidate struct {
	Result
	SourceURL   string
	Domain      string
	Parser      string
	SearchScore float64
}

type aggregateCandidate struct {
	artist         string
	title          string
	hits           int
	distinctDomain int
	bestScore      float64
	scoreSum       float64
	bestTrust      float64
	bestParser     float64
}

func RankCandidates(candidates []Candidate, maxResults int) []Result {
	return rankCandidatesWithThreshold(candidates, maxResults, 1.25)
}

func RelaxedRankCandidates(candidates []Candidate, maxResults int) []Result {
	return rankCandidatesWithThreshold(candidates, maxResults, 0.75)
}

func rankCandidatesWithThreshold(candidates []Candidate, maxResults int, minScore float64) []Result {
	if maxResults <= 0 || len(candidates) == 0 {
		return []Result{}
	}

	agg := make(map[string]*aggregateCandidate)
	domainSets := make(map[string]map[string]struct{})

	for _, c := range candidates {
		artist := normalizeField(strings.TrimSpace(c.Artist))
		title := normalizeField(strings.TrimSpace(c.Title))
		if artist == "" || title == "" {
			continue
		}
		if !fieldLengthOK(artist) || !fieldLengthOK(title) {
			continue
		}
		if hasNoise(artist) || hasNoise(title) {
			continue
		}
		if looksLikeBoilerplate(artist) || looksLikeBoilerplate(title) {
			continue
		}
		if likelyNonSongPair(artist, title) {
			continue
		}

		key := strings.ToLower(artist + "||" + title)
		trust := domainTrustScore(c.Domain)
		parser := parserConfidence(c.Parser)
		quality := candidateScore(c, artist, title)

		entry, ok := agg[key]
		if !ok {
			agg[key] = &aggregateCandidate{
				artist:     artist,
				title:      title,
				hits:       1,
				bestScore:  quality,
				scoreSum:   quality,
				bestTrust:  trust,
				bestParser: parser,
			}
			domainSets[key] = map[string]struct{}{canonicalDomain(c.Domain): {}}
			continue
		}

		entry.hits++
		if quality > entry.bestScore {
			entry.bestScore = quality
		}
		if trust > entry.bestTrust {
			entry.bestTrust = trust
		}
		if parser > entry.bestParser {
			entry.bestParser = parser
		}
		entry.scoreSum += quality
		domainSets[key][canonicalDomain(c.Domain)] = struct{}{}
	}

	ranked := make([]aggregateCandidate, 0, len(agg))
	for key, entry := range agg {
		entry.distinctDomain = len(domainSets[key])
		entry.bestScore += 0.45 * math.Log1p(float64(entry.hits))
		entry.bestScore += 0.25 * float64(entry.distinctDomain-1)
		if entry.distinctDomain == 1 && entry.bestTrust < 0.8 {
			entry.bestScore -= 0.6
		}
		if entry.distinctDomain == 1 && entry.bestParser <= 0.3 {
			entry.bestScore -= 0.4
		}
		ranked = append(ranked, *entry)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].bestScore == ranked[j].bestScore {
			left := strings.ToLower(ranked[i].artist + "||" + ranked[i].title)
			right := strings.ToLower(ranked[j].artist + "||" + ranked[j].title)
			return left < right
		}
		return ranked[i].bestScore > ranked[j].bestScore
	})

	results := make([]Result, 0, maxResults)
	for _, item := range ranked {
		if item.bestScore < minScore {
			continue
		}
		results = append(results, Result{Artist: item.artist, Title: item.title})
		if len(results) >= maxResults {
			break
		}
	}

	return results
}

func candidateScore(c Candidate, artist, title string) float64 {
	score := domainTrustScore(c.Domain)
	score += parserConfidence(c.Parser)
	score += clamp(c.SearchScore/10, 0, 0.3)
	score -= lexicalPenalty(artist)
	score -= lexicalPenalty(title)
	return score
}

func parserConfidence(parser string) float64 {
	switch parser {
	case "wikipedia", "billboard", "musicbrainz", "kworb":
		return 1.3
	case "discogs":
		return 1.1
	case "genius":
		return 0.5
	case "youtube":
		return 0.8
	default:
		return 0.1
	}
}

func domainTrustScore(domain string) float64 {
	domain = canonicalDomain(domain)
	switch {
	case strings.HasSuffix(domain, "wikipedia.org"):
		return 1.6
	case strings.HasSuffix(domain, "musicbrainz.org"):
		return 1.5
	case strings.HasSuffix(domain, "billboard.com"):
		return 1.4
	case strings.HasSuffix(domain, "genius.com"):
		return 0.5
	case strings.HasSuffix(domain, "kworb.net"):
		return 1.3
	case strings.HasSuffix(domain, "discogs.com"):
		return 1.0
	case strings.HasSuffix(domain, "spotify.com"):
		return 0.5
	case strings.HasSuffix(domain, "music.apple.com"):
		return 0.5
	case strings.HasSuffix(domain, "youtube.com"):
		return 0.6
	case strings.HasSuffix(domain, "reddit.com"):
		return 0.2
	default:
		return 0.2
	}
}

func lexicalPenalty(s string) float64 {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 1.2
	}

	lower := strings.ToLower(trimmed)
	penalty := 0.0

	if looksLikeCodeLine(lower) {
		penalty += 1.5
	}
	if strings.Count(lower, "|") >= 2 {
		penalty += 0.4
	}
	if strings.Count(lower, ";") >= 1 {
		penalty += 0.7
	}
	if strings.Contains(lower, "[") || strings.Contains(lower, "]") {
		penalty += 0.8
	}
	if looksLikeBoilerplate(lower) {
		penalty += 1.0
	}
	if strings.Contains(lower, "all songs") || strings.Contains(lower, "popular songs") || strings.Contains(lower, "official store") {
		penalty += 0.7
	}
	if lower == "single" || lower == "album" || lower == "ep" {
		penalty += 1.3
	}
	if strings.Contains(lower, "top albums") || strings.Contains(lower, "spotify top") || strings.Contains(lower, "greatest hits") {
		penalty += 1.0
	}
	if strings.Contains(lower, "final thoughts") || strings.Contains(lower, "song list") || strings.Contains(lower, "all rights administered") {
		penalty += 1.2
	}

	letters := 0
	nonAlphaNum := 0
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
		case r >= '0' && r <= '9' || r == ' ':
			continue
		default:
			nonAlphaNum++
		}
	}
	if letters == 0 {
		penalty += 0.8
	}
	if letters > 0 {
		ratio := float64(nonAlphaNum) / float64(letters)
		if ratio > 0.35 {
			penalty += 0.8
		}
	}

	return penalty
}

func likelyNonSongPair(artist, title string) bool {
	lowerArtist := strings.ToLower(strings.TrimSpace(artist))
	lowerTitle := strings.ToLower(strings.TrimSpace(title))
	combined := lowerArtist + " " + lowerTitle

	if strings.Contains(combined, "greatest of all time") {
		return true
	}
	if strings.Contains(combined, "all rights administered") || strings.Contains(combined, "final thoughts") || strings.Contains(combined, "song list") {
		return true
	}
	if strings.Contains(lowerTitle, "hot 100 songs") || strings.Contains(lowerTitle, "billboard 200 albums") {
		return true
	}
	if (strings.Contains(lowerTitle, "best") || strings.Contains(lowerTitle, "top")) && strings.Contains(lowerTitle, "songs") {
		return true
	}
	if strings.HasSuffix(lowerTitle, " songs") || strings.HasSuffix(lowerTitle, " albums") || strings.HasSuffix(lowerTitle, " artists") {
		return true
	}
	if lowerArtist == "women" || lowerArtist == "music & licensing" || lowerArtist == "wc music corp." {
		return true
	}
	if strings.Contains(lowerArtist, "songs") && strings.Contains(lowerTitle, "thoughts") {
		return true
	}

	return false
}

func clamp(v, minValue, maxValue float64) float64 {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}
