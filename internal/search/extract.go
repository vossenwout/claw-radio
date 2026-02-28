package search

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Result holds a single extracted (artist, title) pair.
type Result struct {
	Artist string
	Title  string
}

var (
	wikitableTableRe = regexp.MustCompile(`(?is)<table\b[^>]*class\s*=\s*["'][^"']*\bwikitable\b[^"']*["'][^>]*>(.*?)</table>`)
	trRe             = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	thRe             = regexp.MustCompile(`(?is)<th\b[^>]*>(.*?)</th>`)
	cellRe           = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>(.*?)</t[dh]>`)
	footnoteRe       = regexp.MustCompile(`\[\d+\]`)
	tagRe            = regexp.MustCompile(`(?is)<[^>]+>`)

	discogsArtistRe = regexp.MustCompile(`(?is)<[a-z0-9]+\b[^>]*class\s*=\s*["'][^"']*\btracklist_track_artists\b[^"']*["'][^>]*>(.*?)</[a-z0-9]+>`)
	discogsTitleRe  = regexp.MustCompile(`(?is)<[a-z0-9]+\b[^>]*class\s*=\s*["'][^"']*\btracklist_track_title\b[^"']*["'][^>]*>(.*?)</[a-z0-9]+>`)
	metaTagRe       = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrRe          = regexp.MustCompile(`(?is)\b([a-zA-Z_:][a-zA-Z0-9_:.-]*)\s*=\s*("([^"]*)"|'([^']*)')`)

	trWithTitleRe = regexp.MustCompile(`(?is)<tr\b[^>]*>.*?<td\b[^>]*class\s*=\s*["'][^"']*\btitle\b[^"']*["'][^>]*>.*?</td>.*?</tr>`)
	tdTitleRe     = regexp.MustCompile(`(?is)<td\b[^>]*class\s*=\s*["'][^"']*\btitle\b[^"']*["'][^>]*>(.*?)</td>`)
	tdArtistRe    = regexp.MustCompile(`(?is)<td\b[^>]*class\s*=\s*["'][^"']*\bartist\b[^"']*["'][^>]*>(.*?)</td>`)

	lineEmDashRe = regexp.MustCompile(`^(.{2,80}?)\s*[–—]\s*(.{2,80}?)$`)
	lineHyphenRe = regexp.MustCompile(`^(.{2,80}?)\s+-\s+(.{2,80}?)$`)
	lineByRe     = regexp.MustCompile(`(?i)^(.{2,60}?)\s+\bby\b\s+(.{2,60}?)$`)
	timestampRe  = regexp.MustCompile(`^\d{1,2}:\d{2}(?::\d{2})?$`)
)

var noiseKeywords = []string{
	"lyrics",
	"karaoke",
	"nightcore",
	"instrumental",
	"cover",
	"remix",
	"mix",
	"1 hour",
	"extended",
	"sped up",
	"slowed",
	"reverb",
	"8d audio",
	"mashup",
	"full album",
	"playlist",
	"megamix",
	"medley",
	"tribute",
}

func Wikitable(pageHTML string) []Result {
	results := make([]Result, 0)
	tables := wikitableTableRe.FindAllStringSubmatch(pageHTML, -1)
	for _, table := range tables {
		rows := trRe.FindAllStringSubmatch(table[1], -1)
		if len(rows) < 2 {
			continue
		}

		headers := thRe.FindAllStringSubmatch(rows[0][1], -1)
		if len(headers) == 0 {
			continue
		}

		artistCol := -1
		titleCol := -1
		for i, header := range headers {
			text := strings.ToLower(cleanText(header[1]))
			if artistCol < 0 && (strings.Contains(text, "artist") || strings.Contains(text, "act") || strings.Contains(text, "performer")) {
				artistCol = i
			}
			if titleCol < 0 && (strings.Contains(text, "single") || strings.Contains(text, "title") || strings.Contains(text, "song")) {
				titleCol = i
			}
		}

		if artistCol < 0 || titleCol < 0 {
			continue
		}

		for _, row := range rows[1:] {
			cells := cellRe.FindAllStringSubmatch(row[1], -1)
			if artistCol >= len(cells) || titleCol >= len(cells) {
				continue
			}

			artist := footnoteRe.ReplaceAllString(cleanText(cells[artistCol][1]), "")
			title := footnoteRe.ReplaceAllString(cleanText(cells[titleCol][1]), "")
			artist = normalizeField(artist)
			title = normalizeField(title)
			if artist == "" || title == "" {
				continue
			}
			results = append(results, Result{Artist: artist, Title: title})
		}
	}

	return results
}

func Discogs(pageHTML string) []Result {
	artists := make([]string, 0)
	for _, match := range discogsArtistRe.FindAllStringSubmatch(pageHTML, -1) {
		text := cleanText(match[1])
		if text != "" {
			artists = append(artists, text)
		}
	}

	titles := make([]string, 0)
	for _, match := range discogsTitleRe.FindAllStringSubmatch(pageHTML, -1) {
		text := cleanText(match[1])
		if text != "" {
			titles = append(titles, text)
		}
	}

	fallbackArtist := ogTitleArtist(pageHTML)
	results := make([]Result, 0, len(titles))
	if len(artists) == 0 {
		for _, title := range titles {
			if fallbackArtist == "" || title == "" {
				continue
			}
			results = append(results, Result{Artist: fallbackArtist, Title: title})
		}
		return results
	}

	n := len(artists)
	if len(titles) < n {
		n = len(titles)
	}
	for i := 0; i < n; i++ {
		if artists[i] == "" || titles[i] == "" {
			continue
		}
		results = append(results, Result{Artist: artists[i], Title: titles[i]})
	}

	return results
}

func MusicBrainz(pageHTML string) []Result {
	releaseArtist := ogTitleArtist(pageHTML)
	results := make([]Result, 0)

	rows := trWithTitleRe.FindAllString(pageHTML, -1)
	for _, row := range rows {
		titleMatch := tdTitleRe.FindStringSubmatch(row)
		if len(titleMatch) < 2 {
			continue
		}

		title := cleanText(titleMatch[1])
		if title == "" {
			continue
		}

		artist := releaseArtist
		if strings.EqualFold(releaseArtist, "Various Artists") {
			artistMatch := tdArtistRe.FindStringSubmatch(row)
			if len(artistMatch) > 1 {
				perTrackArtist := cleanText(artistMatch[1])
				if perTrackArtist != "" {
					artist = perTrackArtist
				}
			}
		}

		if artist == "" {
			continue
		}
		results = append(results, Result{Artist: artist, Title: title})
	}

	return results
}

func Generic(pageHTML string) []Result {
	text := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n",
		"</li>", "\n",
		"</tr>", "\n",
	).Replace(pageHTML)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = tagRe.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)

	results := make([]Result, 0)
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.Join(strings.Fields(rawLine), " ")
		if line == "" {
			continue
		}

		var artist, title string
		switch {
		case lineEmDashRe.MatchString(line):
			matches := lineEmDashRe.FindStringSubmatch(line)
			artist = strings.TrimSpace(matches[1])
			title = strings.TrimSpace(matches[2])
		case lineHyphenRe.MatchString(line):
			matches := lineHyphenRe.FindStringSubmatch(line)
			left := strings.TrimSpace(matches[1])
			right := strings.TrimSpace(matches[2])
			if timestampRe.MatchString(left) && timestampRe.MatchString(right) {
				continue
			}
			artist = left
			title = right
		case lineByRe.MatchString(line):
			matches := lineByRe.FindStringSubmatch(line)
			title = strings.TrimSpace(matches[1])
			artist = strings.TrimSpace(matches[2])
		default:
			continue
		}

		if !fieldLengthOK(artist) || !fieldLengthOK(title) {
			continue
		}
		if hasNoise(artist) || hasNoise(title) {
			continue
		}

		results = append(results, Result{Artist: artist, Title: title})
	}

	return results
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func ogTitleArtist(pageHTML string) string {
	for _, tag := range metaTagRe.FindAllString(pageHTML, -1) {
		attrs := extractAttrs(tag)
		property := strings.ToLower(attrs["property"])
		if property != "og:title" {
			continue
		}
		artist, _ := splitArtistTitle(attrs["content"])
		return strings.TrimSpace(artist)
	}
	return ""
}

func extractAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	matches := attrRe.FindAllStringSubmatch(tag, -1)
	for _, m := range matches {
		name := strings.ToLower(m[1])
		value := m[3]
		if value == "" {
			value = m[4]
		}
		attrs[name] = html.UnescapeString(value)
	}
	return attrs
}

func splitArtistTitle(s string) (string, string) {
	for _, sep := range []string{" – ", " — ", " - "} {
		parts := strings.SplitN(s, sep, 2)
		if len(parts) != 2 {
			continue
		}
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(s), ""
}

func hasNoise(s string) bool {
	lower := strings.ToLower(s)
	for _, keyword := range noiseKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func fieldLengthOK(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 2 && n <= 80
}

func normalizeField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
			(strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) ||
			(strings.HasPrefix(s, "“") && strings.HasSuffix(s, "”")) {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}
