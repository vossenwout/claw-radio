package search

import (
	"regexp"
	"strings"
)

type QueryMode string

const (
	ModeRaw        QueryMode = "raw"
	ModeArtistTop  QueryMode = "artist-top"
	ModeArtistYear QueryMode = "artist-year"
	ModeChartYear  QueryMode = "chart-year"
	ModeGenreTop   QueryMode = "genre-top"
)

var nonWordRe = regexp.MustCompile(`[^a-z0-9]+`)

func BuildProfile(mode QueryMode, raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}

	switch mode {
	case ModeArtistTop:
		artist := normalizeArtistSeed(trimmed)
		queries := []string{
			artist + " most popular songs",
			artist + " top songs billboard",
			artist + " songs wikipedia",
			artist + " spotify top songs kworb",
		}
		return dedupeStable(queries)
	case ModeArtistYear:
		artist := normalizeArtistSeed(removeYears(trimmed))
		year := firstYear(trimmed)
		if artist == "" {
			return []string{trimmed}
		}
		if year == "" {
			queries := []string{
				artist + " songs",
				artist + " discography singles",
				artist + " songs wikipedia",
			}
			return dedupeStable(queries)
		}
		queries := []string{
			artist + " most popular songs",
			artist + " top songs billboard",
			artist + " songs wikipedia",
			artist + " spotify top songs kworb",
			artist + " " + year + " songs",
			artist + " songs released in " + year,
			artist + " " + year + " tracklist",
			artist + " discography " + year,
			artist + " " + year + " site:wikipedia.org",
		}
		return dedupeStable(queries)
	case ModeChartYear:
		if !looksLikeChartIntent(trimmed) {
			if firstYear(trimmed) != "" && !looksLikeChartIntent(removeYears(trimmed)) {
				return BuildProfile(ModeArtistYear, trimmed)
			}
			queries := []string{
				trimmed,
				trimmed + " songs",
				trimmed + " site:wikipedia.org",
			}
			return dedupeStable(queries)
		}

		queries := []string{
			trimmed,
			trimmed + " site:wikipedia.org",
			trimmed + " year-end chart list",
			trimmed + " billboard list",
		}
		return dedupeStable(queries)
	case ModeGenreTop:
		queries := []string{
			trimmed,
			trimmed + " best songs",
			trimmed + " essential songs",
			trimmed + " site:wikipedia.org",
			trimmed + " classic tracks",
		}
		return dedupeStable(queries)
	default:
		return []string{trimmed}
	}
}

func ParseMode(raw string) QueryMode {
	switch QueryMode(strings.TrimSpace(strings.ToLower(raw))) {
	case ModeRaw, "":
		return ModeRaw
	case ModeArtistTop:
		return ModeArtistTop
	case ModeArtistYear:
		return ModeArtistYear
	case ModeChartYear:
		return ModeChartYear
	case ModeGenreTop:
		return ModeGenreTop
	default:
		return ""
	}
}

func firstYear(query string) string {
	for _, token := range strings.Fields(strings.TrimSpace(query)) {
		if len(token) != 4 {
			continue
		}
		if token[0] != '1' && token[0] != '2' {
			continue
		}
		isDigits := true
		for i := 0; i < 4; i++ {
			if token[i] < '0' || token[i] > '9' {
				isDigits = false
				break
			}
		}
		if isDigits {
			return token
		}
	}
	return ""
}

func removeYears(query string) string {
	out := make([]string, 0)
	for _, token := range strings.Fields(strings.TrimSpace(query)) {
		if len(token) == 4 {
			isDigits := true
			for i := 0; i < 4; i++ {
				if token[i] < '0' || token[i] > '9' {
					isDigits = false
					break
				}
			}
			if isDigits {
				continue
			}
		}
		out = append(out, token)
	}
	return strings.Join(out, " ")
}

func ExpandWithSuggestions(raw string, suggestions []string, max int) []string {
	if max <= 0 {
		return []string{}
	}

	rawTokens := tokenize(raw)
	if len(rawTokens) == 0 {
		return []string{}
	}

	minOverlap := 2
	if len(rawTokens) <= 2 {
		minOverlap = 1
	}

	accepted := make([]string, 0, max)
	seen := map[string]struct{}{strings.ToLower(strings.TrimSpace(raw)): {}}

	for _, suggestion := range suggestions {
		candidate := strings.TrimSpace(suggestion)
		if candidate == "" {
			continue
		}
		lowerCandidate := strings.ToLower(candidate)
		if _, ok := seen[lowerCandidate]; ok {
			continue
		}

		overlap := tokenOverlap(rawTokens, tokenize(candidate))
		if overlap < minOverlap {
			continue
		}

		seen[lowerCandidate] = struct{}{}
		accepted = append(accepted, candidate)
		if len(accepted) >= max {
			break
		}
	}

	return accepted
}

func normalizeArtistSeed(s string) string {
	words := strings.Fields(strings.TrimSpace(s))
	if len(words) == 0 {
		return strings.TrimSpace(s)
	}

	stop := map[string]struct{}{
		"song":    {},
		"songs":   {},
		"music":   {},
		"top":     {},
		"best":    {},
		"popular": {},
		"most":    {},
	}

	for len(words) > 1 {
		last := strings.TrimSpace(strings.ToLower(words[len(words)-1]))
		if _, ok := stop[last]; !ok {
			break
		}
		words = words[:len(words)-1]
	}

	return strings.Join(words, " ")
}

func tokenize(s string) []string {
	normalized := strings.ToLower(nonWordRe.ReplaceAllString(strings.TrimSpace(s), " "))
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return []string{}
	}
	return fields
}

func tokenOverlap(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, tok := range a {
		set[tok] = struct{}{}
	}
	overlap := 0
	seen := make(map[string]struct{}, len(b))
	for _, tok := range b {
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		if _, ok := set[tok]; ok {
			overlap++
		}
	}
	return overlap
}

func dedupeStable(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func NormalizeEngineList(values []string) []string {
	flattened := make([]string, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(strings.ToLower(part))
			if trimmed == "" {
				continue
			}
			flattened = append(flattened, trimmed)
		}
	}
	return dedupeStable(flattened)
}

func looksLikeChartIntent(query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return false
	}
	chartKeywords := []string{
		"chart",
		"year-end",
		"year end",
		"billboard",
		"hot 100",
		"top 100",
		"top songs",
		"singles",
	}
	for _, keyword := range chartKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}
