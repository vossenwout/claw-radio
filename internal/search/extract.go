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
	footnoteRe       = regexp.MustCompile(`\[\s*\d+\s*\]`)
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
	lineRankedRe = regexp.MustCompile(`^\d{1,3}\.\s+(.{2,100}?)\s+-\s+(.{2,140}?)(?:\s+\*+)?$`)
	rankPrefixRe = regexp.MustCompile(`^\d{1,3}\.\s+`)
	timestampRe  = regexp.MustCompile(`^\d{1,2}:\d{2}(?::\d{2})?$`)
	timeOfDayRe  = regexp.MustCompile(`(?i)\b\d{1,2}:\d{2}\s*(am|pm)\b`)
	monthDateRe  = regexp.MustCompile(`(?i)\b\d{1,2}\s+(january|february|march|april|may|june|july|august|september|october|november|december)\b`)

	titleTagRe          = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	h1Re                = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	kworbSongTableRe    = regexp.MustCompile(`(?is)<table\b[^>]*class\s*=\s*["'][^"']*\baddpos\b[^"']*\bsortable\b[^"']*["'][^>]*>(.*?)</table>`)
	kworbTrackAnchorRe  = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']https?://open\.spotify\.com/track/[^"']+["'][^>]*>(.*?)</a>`)
	geniusSongLinkRe    = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']https?://genius\.com/[^"']+["'][^>]*>(.*?)</a>`)
	youtubeTitleTrimRe  = regexp.MustCompile(`(?i)\s*[-–—|]\s*youtube\s*$`)
	trailingStatsTailRe = regexp.MustCompile(`\s+\d[\d,]*(?:\s+\d[\d,]*)+$`)
	citationTailRe      = regexp.MustCompile(`\s*(?:"|')?\s*(?:↑\s*)?\[\s*\d+\s*\]\s*$`)
	citationOpenTailRe  = regexp.MustCompile(`\s*(?:"|')?\s*(?:↑\s*)?\[\s*\d+\s*$`)
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
	"more stories",
	"apple music",
	"official store",
	"chart history",
	"much appreciated",
	"any thoughts",
	"forum",
	"thread",
	"illustration",
	"final thoughts",
	"all rights administered",
	"music & licensing",
	"song list",
	"greatest of all time hot 100 songs",
	"greatest of all time billboard 200 albums",
	"trusted",
	"songs ranked",
	"best songs of",
}

func Wikitable(pageHTML string) []Result {
	results := make([]Result, 0)
	tables := wikitableTableRe.FindAllStringSubmatch(pageHTML, -1)
	for _, table := range tables {
		rows := trRe.FindAllStringSubmatch(table[1], -1)
		if len(rows) < 2 {
			continue
		}

		headerRow := -1
		var headers [][]string
		for rowIndex, row := range rows {
			headers = thRe.FindAllStringSubmatch(row[1], -1)
			if len(headers) > 0 {
				headerRow = rowIndex
				break
			}
		}
		if headerRow < 0 || len(headers) == 0 {
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

		for _, row := range rows[headerRow+1:] {
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
	releaseArtist := musicBrainzReleaseArtist(pageHTML)
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
	text := visibleText(pageHTML)
	results := make([]Result, 0)
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.Join(strings.Fields(rawLine), " ")
		if line == "" || looksLikeCodeLine(line) || looksLikeBoilerplate(line) {
			continue
		}

		artist, title, ok := extractPairFromLine(line)
		if !ok {
			continue
		}

		artist = normalizeCandidateField(artist)
		title = normalizeCandidateField(title)
		if looksLikeTimestampOrDate(artist) || looksLikeTimestampOrDate(title) {
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

		results = append(results, Result{Artist: artist, Title: title})
	}

	return results
}

func Billboard(pageHTML string) []Result {
	text := visibleText(pageHTML)
	results := make([]Result, 0)

	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.Join(strings.Fields(rawLine), " ")
		if line == "" || looksLikeCodeLine(line) || looksLikeBoilerplate(line) {
			continue
		}
		if !lineRankedRe.MatchString(line) {
			continue
		}

		matches := lineRankedRe.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		artist := normalizeCandidateField(matches[1])
		title := normalizeCandidateField(matches[2])
		if !fieldLengthOK(artist) || !fieldLengthOK(title) {
			continue
		}
		if hasNoise(artist) || hasNoise(title) {
			continue
		}

		results = append(results, Result{Artist: artist, Title: title})
	}

	if len(results) == 0 {
		return Generic(pageHTML)
	}
	return results
}

func Kworb(pageHTML string) []Result {
	table := kworbSongTableRe.FindStringSubmatch(pageHTML)
	if len(table) < 2 {
		return Generic(pageHTML)
	}

	artist := kworbArtist(pageHTML)

	results := make([]Result, 0)
	for _, match := range kworbTrackAnchorRe.FindAllStringSubmatch(table[1], -1) {
		title := normalizeCandidateField(cleanText(match[1]))
		if title == "" || !fieldLengthOK(title) || hasNoise(title) || looksLikeBoilerplate(title) {
			continue
		}
		if artist == "" {
			continue
		}
		results = append(results, Result{Artist: artist, Title: title})
	}

	if len(results) == 0 {
		return Generic(pageHTML)
	}

	return results
}

func GeniusSongs(pageHTML string) []Result {
	results := make([]Result, 0)
	for _, match := range geniusSongLinkRe.FindAllStringSubmatch(pageHTML, -1) {
		text := normalizeCandidateField(cleanText(match[1]))
		if text == "" || looksLikeCodeLine(text) || looksLikeBoilerplate(text) {
			continue
		}
		artist, title, ok := extractPairFromLine(text)
		if !ok {
			continue
		}
		artist = normalizeCandidateField(artist)
		title = normalizeCandidateField(title)
		if !fieldLengthOK(artist) || !fieldLengthOK(title) {
			continue
		}
		if hasNoise(artist) || hasNoise(title) {
			continue
		}
		results = append(results, Result{Artist: artist, Title: title})
	}

	if len(results) == 0 {
		return Generic(pageHTML)
	}
	return results
}

func YouTube(pageHTML string) []Result {
	match := titleTagRe.FindStringSubmatch(pageHTML)
	if len(match) < 2 {
		return Generic(pageHTML)
	}

	title := normalizeCandidateField(cleanText(match[1]))
	title = youtubeTitleTrimRe.ReplaceAllString(title, "")
	artist, trackTitle, ok := extractPairFromLine(title)
	if !ok {
		return []Result{}
	}

	artist = normalizeCandidateField(artist)
	trackTitle = normalizeCandidateField(trackTitle)
	if !fieldLengthOK(artist) || !fieldLengthOK(trackTitle) {
		return []Result{}
	}
	if hasNoise(artist) || hasNoise(trackTitle) {
		return []Result{}
	}

	return []Result{{Artist: artist, Title: trackTitle}}
}

func visibleText(pageHTML string) string {
	cleaned := stripNonContentBlocks(pageHTML)
	text := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n",
		"</li>", "\n",
		"</tr>", "\n",
		"</div>", "\n",
		"</h1>", "\n",
		"</h2>", "\n",
		"</h3>", "\n",
	).Replace(cleaned)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = tagRe.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return text
}

func extractPairFromLine(line string) (artist string, title string, ok bool) {
	line = strings.TrimSpace(line)
	switch {
	case lineEmDashRe.MatchString(line):
		matches := lineEmDashRe.FindStringSubmatch(line)
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), true
	case lineHyphenRe.MatchString(line):
		matches := lineHyphenRe.FindStringSubmatch(line)
		left := strings.TrimSpace(matches[1])
		right := strings.TrimSpace(matches[2])
		if timestampRe.MatchString(left) && timestampRe.MatchString(right) {
			return "", "", false
		}
		return left, right, true
	case lineByRe.MatchString(line):
		matches := lineByRe.FindStringSubmatch(line)
		return strings.TrimSpace(matches[2]), strings.TrimSpace(matches[1]), true
	default:
		return "", "", false
	}
}

func normalizeCandidateField(s string) string {
	s = normalizeField(s)
	s = rankPrefixRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "�", "")
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimSpace(s)
	s = citationTailRe.ReplaceAllString(s, "")
	s = citationOpenTailRe.ReplaceAllString(s, "")
	s = trailingStatsTailRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.TrimSuffix(s, "*"))
	return normalizeField(s)
}

func kworbArtist(pageHTML string) string {
	titleMatch := titleTagRe.FindStringSubmatch(pageHTML)
	if len(titleMatch) < 2 {
		return ""
	}
	title := cleanText(titleMatch[1])
	parts := strings.Split(title, " - ")
	if len(parts) < 2 {
		return ""
	}
	artist := normalizeCandidateField(parts[0])
	if !fieldLengthOK(artist) || looksLikeBoilerplate(artist) {
		return ""
	}
	return artist
}

func looksLikeTimestampOrDate(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	if timeOfDayRe.MatchString(lower) {
		return true
	}
	if monthDateRe.MatchString(lower) {
		return true
	}
	return false
}

func musicBrainzReleaseArtist(pageHTML string) string {
	if artist := ogTitleArtist(pageHTML); artist != "" {
		return artist
	}

	titleMatch := titleTagRe.FindStringSubmatch(pageHTML)
	if len(titleMatch) > 1 {
		title := cleanText(titleMatch[1])
		if strings.Contains(title, " by ") {
			parts := strings.Split(title, " by ")
			artistPart := strings.TrimSpace(parts[len(parts)-1])
			artistPart = strings.TrimSuffix(artistPart, " - MusicBrainz")
			artistPart = normalizeCandidateField(artistPart)
			if fieldLengthOK(artistPart) {
				return artistPart
			}
		}
	}

	h1Match := h1Re.FindStringSubmatch(pageHTML)
	if len(h1Match) > 1 {
		text := normalizeCandidateField(cleanText(h1Match[1]))
		if fieldLengthOK(text) && !looksLikeBoilerplate(text) {
			return text
		}
	}

	return ""
}

func cleanText(s string) string {
	s = stripNonContentBlocks(s)
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "↑", " ")
	s = strings.ReplaceAll(s, "↵", " ")
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
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '\uFFFD' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	s = strings.Trim(s, " \t\n\r\"'`“”‘’{}<>•*|\\/;,~")
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
