package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxSearchHits    = 20
	defaultMaxPages         = 20
	defaultFetchConcurrency = 6
	defaultUserAgent        = "claw-radio/1.0 (+https://github.com/vossenwout/claw-radio)"
)

type Client struct {
	searxngURL           string
	httpClient           *http.Client
	maxSearchHits        int
	maxPages             int
	fetchConcurrency     int
	userAgent            string
	enableQueryExpansion bool
}

type ClientOption func(*Client)

type SearchOptions struct {
	Mode              QueryMode
	Modes             []QueryMode
	ExpandSuggestions bool
	MaxPages          int
	Engines           []string
}

type Stats struct {
	Queries              []string
	PagesAttempted       int
	PagesFetched         int
	PagesFailed          int
	FailureReasons       map[string]int
	CandidatesBeforeRank int
	CandidatesAfterRank  int
	UnresponsiveEngines  []string
	RequestedEngines     []string
}

type SearchHit struct {
	URL      string
	Title    string
	Content  string
	Engine   string
	Category string
	Score    float64
	Domain   string
	index    int
}

func WithMaxSearchHits(v int) ClientOption {
	return func(c *Client) {
		if v > 0 {
			c.maxSearchHits = v
		}
	}
}

func WithMaxPages(v int) ClientOption {
	return func(c *Client) {
		if v > 0 {
			c.maxPages = v
		}
	}
}

func WithFetchConcurrency(v int) ClientOption {
	return func(c *Client) {
		if v > 0 {
			c.fetchConcurrency = v
		}
	}
}

func WithRequestTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if timeout > 0 {
			c.httpClient.Timeout = timeout
		}
	}
}

func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) {
		trimmed := strings.TrimSpace(userAgent)
		if trimmed != "" {
			c.userAgent = trimmed
		}
	}
}

func WithEnableQueryExpansion(enabled bool) ClientOption {
	return func(c *Client) {
		c.enableQueryExpansion = enabled
	}
}

func NewClient(searxngURL string, opts ...ClientOption) *Client {
	trimmed := strings.TrimRight(strings.TrimSpace(searxngURL), "/")
	client := &Client{
		searxngURL:       trimmed,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		maxSearchHits:    defaultMaxSearchHits,
		maxPages:         defaultMaxPages,
		fetchConcurrency: defaultFetchConcurrency,
		userAgent:        defaultUserAgent,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}

	if client.maxSearchHits <= 0 {
		client.maxSearchHits = defaultMaxSearchHits
	}
	if client.maxPages <= 0 {
		client.maxPages = defaultMaxPages
	}
	if client.fetchConcurrency <= 0 {
		client.fetchConcurrency = defaultFetchConcurrency
	}
	if strings.TrimSpace(client.userAgent) == "" {
		client.userAgent = defaultUserAgent
	}

	return client
}

func (c *Client) Search(query string, maxResults int) ([]Result, error) {
	results, _, err := c.SearchWithStats(query, maxResults)
	return results, err
}

func (c *Client) SearchWithStats(query string, maxResults int) ([]Result, int, error) {
	results, stats, err := c.SearchDetailed(query, maxResults, SearchOptions{Mode: ModeRaw})
	if err != nil {
		return nil, 0, err
	}
	return results, stats.PagesAttempted, nil
}

func (c *Client) SearchDetailed(query string, maxResults int, options SearchOptions) ([]Result, Stats, error) {
	stats := Stats{FailureReasons: map[string]int{}}
	if maxResults <= 0 {
		return []Result{}, stats, nil
	}

	modes := normalizeSearchModes(options)
	plannedQueries := make([]string, 0)
	for _, mode := range modes {
		plannedQueries = append(plannedQueries, BuildProfile(mode, query)...)
	}
	plannedQueries = dedupeStable(plannedQueries)
	if len(plannedQueries) == 0 {
		return []Result{}, stats, nil
	}

	allowSuggestions := options.ExpandSuggestions || c.enableQueryExpansion
	requestedEngines := NormalizeEngineList(options.Engines)
	stats.RequestedEngines = append([]string(nil), requestedEngines...)
	allHits := make([]SearchHit, 0)
	seenQueries := map[string]struct{}{}

	unresponsiveSet := map[string]struct{}{}

	for i := 0; i < len(plannedQueries); i++ {
		currentQuery := strings.TrimSpace(plannedQueries[i])
		if currentQuery == "" {
			continue
		}
		key := strings.ToLower(currentQuery)
		if _, ok := seenQueries[key]; ok {
			continue
		}
		seenQueries[key] = struct{}{}

		hits, suggestions, unresponsive, err := c.fetchResultsWithRetry(currentQuery, c.maxSearchHits, requestedEngines)
		if err != nil {
			return nil, stats, err
		}
		allHits = append(allHits, hits...)
		for _, item := range unresponsive {
			if strings.TrimSpace(item) == "" {
				continue
			}
			unresponsiveSet[item] = struct{}{}
		}

		if allowSuggestions && i == 0 {
			extraQueries := ExpandWithSuggestions(query, suggestions, 1)
			for _, extra := range extraQueries {
				if _, ok := seenQueries[strings.ToLower(extra)]; ok {
					continue
				}
				plannedQueries = append(plannedQueries, extra)
			}
		}
	}

	stats.Queries = append([]string(nil), dedupeStable(plannedQueries)...)
	if len(unresponsiveSet) > 0 {
		engines := make([]string, 0, len(unresponsiveSet))
		for item := range unresponsiveSet {
			engines = append(engines, item)
		}
		sort.Strings(engines)
		stats.UnresponsiveEngines = engines
	}

	maxPages := c.maxPages
	if options.MaxPages > 0 {
		maxPages = options.MaxPages
	}
	selectedHits := c.selectHits(allHits, maxPages)
	stats.PagesAttempted = len(selectedHits)

	candidates, pagesFetched, failureReasons := c.fetchCandidates(selectedHits)
	stats.PagesFetched = pagesFetched
	stats.PagesFailed = stats.PagesAttempted - pagesFetched
	stats.FailureReasons = failureReasons

	candidatesForRank := candidates
	if shouldApplyArtistFilters(modes) {
		filteredCandidates := filterArtistCandidates(candidates, query)
		if len(filteredCandidates) > 0 {
			candidatesForRank = filteredCandidates
		}
	}

	stats.CandidatesBeforeRank = len(candidatesForRank)

	ranked := RankCandidates(candidatesForRank, maxResults)
	if len(ranked) < minDesiredResultCount(maxResults) {
		relaxed := RelaxedRankCandidates(candidatesForRank, maxResults)
		if len(relaxed) > len(ranked) {
			ranked = relaxed
		}
	}
	if shouldApplyArtistFilters(modes) {
		ranked = filterArtistModeResults(ranked, query, maxResults)
	}
	if cap := artistDiversityCap(modes); cap > 0 {
		ranked = diversifyByArtist(ranked, maxResults, cap)
	}
	stats.CandidatesAfterRank = len(ranked)

	return ranked, stats, nil
}

func normalizeSearchModes(options SearchOptions) []QueryMode {
	if len(options.Modes) > 0 {
		modes := make([]QueryMode, 0, len(options.Modes))
		seen := map[QueryMode]struct{}{}
		for _, mode := range options.Modes {
			parsed := ParseMode(string(mode))
			if parsed == "" {
				continue
			}
			if _, ok := seen[parsed]; ok {
				continue
			}
			seen[parsed] = struct{}{}
			modes = append(modes, parsed)
		}
		if len(modes) > 0 {
			return modes
		}
	}

	single := ParseMode(string(options.Mode))
	if single == "" {
		single = ModeRaw
	}
	return []QueryMode{single}
}

func shouldApplyArtistFilters(modes []QueryMode) bool {
	hasArtistConstraint := false
	hasArtistYear := false
	hasChartOrGenre := false
	for _, mode := range modes {
		if mode == ModeArtistTop || mode == ModeArtistYear {
			hasArtistConstraint = true
		}
		if mode == ModeArtistYear {
			hasArtistYear = true
		}
		if mode == ModeChartYear || mode == ModeGenreTop {
			hasChartOrGenre = true
		}
	}
	if hasChartOrGenre && !hasArtistYear {
		return false
	}
	return hasArtistConstraint
}

func artistDiversityCap(modes []QueryMode) int {
	hasArtistMode := false
	hasChart := false
	hasGenre := false
	for _, mode := range modes {
		switch mode {
		case ModeArtistTop, ModeArtistYear:
			hasArtistMode = true
		case ModeChartYear:
			hasChart = true
		case ModeGenreTop:
			hasGenre = true
		}
	}

	if hasArtistMode && !hasChart && !hasGenre {
		return 0
	}
	if hasChart || hasGenre {
		if hasChart && hasGenre {
			return 2
		}
		return 3
	}
	return 0
}

func diversifyByArtist(results []Result, maxResults int, perArtistCap int) []Result {
	if perArtistCap <= 0 || len(results) == 0 {
		return results
	}

	primary := make([]Result, 0, len(results))
	remainder := make([]Result, 0, len(results))
	counts := map[string]int{}

	for _, item := range results {
		key := primaryArtistKey(item.Artist)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(item.Artist))
		}
		if key == "" {
			continue
		}
		if counts[key] < perArtistCap {
			counts[key]++
			primary = append(primary, item)
		} else {
			remainder = append(remainder, item)
		}
		if len(primary) >= maxResults {
			return primary[:maxResults]
		}
	}

	if len(primary) >= minDesiredResultCount(maxResults) {
		return primary
	}

	for _, item := range remainder {
		primary = append(primary, item)
		if len(primary) >= maxResults {
			break
		}
	}

	return primary
}

func primaryArtistKey(artist string) string {
	lower := strings.ToLower(strings.TrimSpace(artist))
	if lower == "" {
		return ""
	}
	for _, sep := range []string{" featuring ", " feat. ", " feat ", " with ", " x ", " & ", ","} {
		if idx := strings.Index(lower, sep); idx > 0 {
			lower = strings.TrimSpace(lower[:idx])
			break
		}
	}
	return lower
}

func (c *Client) fetchResults(query string, n int, engines []string) ([]SearchHit, []string, []string, error) {
	if n <= 0 {
		return []SearchHit{}, []string{}, []string{}, nil
	}

	base := strings.TrimRight(c.searxngURL, "/")
	values := url.Values{}
	values.Set("q", strings.TrimSpace(query))
	values.Set("format", "json")
	normalizedEngines := NormalizeEngineList(engines)
	if len(normalizedEngines) > 0 {
		values.Set("engines", strings.Join(normalizedEngines, ","))
	}
	endpoint := base + "/search?" + values.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build searxng request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("searxng unreachable at %s: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("searxng unreachable at %s: status %d", base, resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			URL      string  `json:"url"`
			Title    string  `json:"title"`
			Content  string  `json:"content"`
			Engine   string  `json:"engine"`
			Category string  `json:"category"`
			Score    float64 `json:"score"`
		} `json:"results"`
		Suggestions         []string   `json:"suggestions"`
		UnresponsiveEngines [][]string `json:"unresponsive_engines"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, nil, nil, fmt.Errorf("parse searxng response from %s: %w", endpoint, err)
	}

	hits := make([]SearchHit, 0, n)
	for idx, item := range payload.Results {
		rawURL := strings.TrimSpace(item.URL)
		if rawURL == "" {
			continue
		}
		hits = append(hits, SearchHit{
			URL:      rawURL,
			Title:    strings.TrimSpace(item.Title),
			Content:  strings.TrimSpace(item.Content),
			Engine:   strings.TrimSpace(item.Engine),
			Category: strings.TrimSpace(item.Category),
			Score:    item.Score,
			Domain:   canonicalDomainFromURL(rawURL),
			index:    idx,
		})
		if len(hits) >= n {
			break
		}
	}

	return hits, payload.Suggestions, flattenUnresponsiveEngines(payload.UnresponsiveEngines), nil
}

func (c *Client) fetchResultsWithRetry(query string, n int, engines []string) ([]SearchHit, []string, []string, error) {
	hits, suggestions, unresponsive, err := c.fetchResults(query, n, engines)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(hits) > 0 || len(unresponsive) == 0 {
		return hits, suggestions, unresponsive, nil
	}

	time.Sleep(250 * time.Millisecond)
	retryHits, retrySuggestions, retryUnresponsive, retryErr := c.fetchResults(query, n, engines)
	if retryErr != nil {
		return hits, suggestions, unresponsive, nil
	}
	if len(retrySuggestions) > len(suggestions) {
		suggestions = retrySuggestions
	}
	mergedUnresponsive := append([]string{}, unresponsive...)
	mergedUnresponsive = append(mergedUnresponsive, retryUnresponsive...)
	mergedUnresponsive = dedupeStable(mergedUnresponsive)
	if len(retryHits) > 0 {
		return retryHits, suggestions, mergedUnresponsive, nil
	}
	return hits, suggestions, mergedUnresponsive, nil
}

func flattenUnresponsiveEngines(raw [][]string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	items := make([]string, 0, len(raw))
	for _, entry := range raw {
		if len(entry) >= 2 {
			items = append(items, strings.TrimSpace(entry[0])+": "+strings.TrimSpace(entry[1]))
			continue
		}
		if len(entry) == 1 {
			items = append(items, strings.TrimSpace(entry[0]))
		}
	}
	return items
}

func (c *Client) selectHits(hits []SearchHit, maxPages int) []SearchHit {
	if maxPages <= 0 {
		return []SearchHit{}
	}

	type scoredHit struct {
		hit   SearchHit
		score float64
	}

	bestByURL := make(map[string]scoredHit)
	order := make([]string, 0, len(hits))

	for _, hit := range hits {
		normURL := normalizeURL(hit.URL)
		if normURL == "" {
			continue
		}
		hit.URL = normURL
		hit.Domain = canonicalDomainFromURL(normURL)

		score := domainTrustScore(hit.Domain) + clamp(hit.Score/10, 0, 0.4)
		existing, exists := bestByURL[normURL]
		if !exists {
			bestByURL[normURL] = scoredHit{hit: hit, score: score}
			order = append(order, normURL)
			continue
		}
		if score > existing.score {
			bestByURL[normURL] = scoredHit{hit: hit, score: score}
		}
	}

	scored := make([]scoredHit, 0, len(bestByURL))
	for _, key := range order {
		if item, ok := bestByURL[key]; ok {
			scored = append(scored, item)
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].hit.index < scored[j].hit.index
		}
		return scored[i].score > scored[j].score
	})

	selected := make([]SearchHit, 0, maxPages)
	for _, item := range scored {
		selected = append(selected, item.hit)
		if len(selected) >= maxPages {
			break
		}
	}

	return selected
}

func (c *Client) fetchCandidates(hits []SearchHit) ([]Candidate, int, map[string]int) {
	if len(hits) == 0 {
		return []Candidate{}, 0, map[string]int{}
	}

	workerCount := c.fetchConcurrency
	if workerCount <= 0 {
		workerCount = defaultFetchConcurrency
	}
	if workerCount > len(hits) {
		workerCount = len(hits)
	}

	type pageOutcome struct {
		candidates []Candidate
		reason     string
		fetched    bool
	}

	jobs := make(chan SearchHit)
	outcomes := make(chan pageOutcome)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hit := range jobs {
				results, parser, err := c.fetchAndExtractWithMeta(hit.URL)
				if err != nil {
					outcomes <- pageOutcome{reason: classifyFetchError(err)}
					continue
				}

				candidates := make([]Candidate, 0, len(results))
				for _, item := range results {
					candidates = append(candidates, Candidate{
						Result:      item,
						SourceURL:   hit.URL,
						Domain:      hit.Domain,
						Parser:      parser,
						SearchScore: hit.Score,
					})
				}

				outcomes <- pageOutcome{candidates: candidates, fetched: true}
			}
		}()
	}

	go func() {
		for _, hit := range hits {
			jobs <- hit
		}
		close(jobs)
		wg.Wait()
		close(outcomes)
	}()

	allCandidates := make([]Candidate, 0)
	fetched := 0
	reasons := map[string]int{}
	for outcome := range outcomes {
		if outcome.fetched {
			fetched++
			allCandidates = append(allCandidates, outcome.candidates...)
			continue
		}
		reasons[outcome.reason]++
	}

	return allCandidates, fetched, reasons
}

func (c *Client) fetchAndExtract(rawURL string) ([]Result, error) {
	results, _, err := c.fetchAndExtractWithMeta(rawURL)
	return results, err
}

func (c *Client) fetchAndExtractWithMeta(rawURL string) ([]Result, string, error) {
	html, err := c.fetchHTML(rawURL)
	if err != nil {
		return nil, "", err
	}

	parser := parserForURL(rawURL)
	switch parser {
	case "wikipedia":
		return Wikitable(html), parser, nil
	case "discogs":
		return Discogs(html), parser, nil
	case "musicbrainz":
		return MusicBrainz(html), parser, nil
	case "billboard":
		return Billboard(html), parser, nil
	case "kworb":
		return Kworb(html), parser, nil
	case "genius":
		return []Result{}, parser, nil
	case "youtube":
		return YouTube(html), parser, nil
	default:
		return Generic(html), parser, nil
	}
}

func parserForURL(rawURL string) string {
	lowerURL := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lowerURL, "wikipedia.org"):
		return "wikipedia"
	case strings.Contains(lowerURL, "discogs.com"):
		return "discogs"
	case strings.Contains(lowerURL, "musicbrainz.org"):
		return "musicbrainz"
	case strings.Contains(lowerURL, "billboard.com"):
		return "billboard"
	case strings.Contains(lowerURL, "kworb.net"):
		return "kworb"
	case strings.Contains(lowerURL, "genius.com"):
		return "genius"
	case strings.Contains(lowerURL, "youtube.com") || strings.Contains(lowerURL, "youtu.be"):
		return "youtube"
	default:
		return "generic"
	}
}

func (c *Client) fetchHTML(rawURL string) (string, error) {
	const maxAttempts = 2
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return "", fmt.Errorf("build request for %s: %w", rawURL, err)
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request %s: %w", rawURL, err)
			if attempt < maxAttempts-1 && shouldRetryError(err) {
				time.Sleep(time.Duration(150*(attempt+1)) * time.Millisecond)
				continue
			}
			break
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d for %s", resp.StatusCode, rawURL)
			if attempt < maxAttempts-1 && shouldRetryStatus(resp.StatusCode) {
				time.Sleep(time.Duration(150*(attempt+1)) * time.Millisecond)
				continue
			}
			break
		}

		if readErr != nil {
			lastErr = fmt.Errorf("read %s: %w", rawURL, readErr)
			if attempt < maxAttempts-1 {
				time.Sleep(time.Duration(150*(attempt+1)) * time.Millisecond)
				continue
			}
			break
		}

		return string(body), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("request %s failed", rawURL)
	}
	return "", lastErr
}

func shouldRetryStatus(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status <= 599
}

func shouldRetryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "timeout")
}

func classifyFetchError(err error) string {
	if err == nil {
		return "unknown"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "status 403"):
		return "status 403"
	case strings.Contains(msg, "status 404"):
		return "status 404"
	case strings.Contains(msg, "status 429"):
		return "status 429"
	case strings.Contains(msg, "status 5"):
		return "status 5xx"
	case strings.Contains(msg, "timeout"):
		return "timeout"
	default:
		return "fetch error"
	}
}

func normalizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}

	query := parsed.Query()
	for key := range query {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "utm_") ||
			lowerKey == "fbclid" ||
			lowerKey == "gclid" ||
			lowerKey == "srsltid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()

	if parsed.Fragment != "" {
		parsed.Fragment = ""
	}

	return parsed.String()
}

func filterArtistModeResults(results []Result, artistQuery string, maxResults int) []Result {
	seed := normalizeArtistSeed(artistQuery)
	tokens := tokenize(seed)
	if len(tokens) == 0 {
		return results
	}

	filtered := make([]Result, 0, len(results))
	for _, result := range results {
		if artistModeRelevant(result, seed, tokens) {
			filtered = append(filtered, result)
		}
		if len(filtered) >= maxResults {
			break
		}
	}

	if len(filtered) == 0 {
		return results
	}
	return filtered
}

func artistModeRelevant(result Result, seed string, tokens []string) bool {
	artist := strings.ToLower(result.Artist)
	title := strings.ToLower(result.Title)
	seed = strings.ToLower(strings.TrimSpace(seed))

	if seed != "" && strings.Contains(artist, seed) {
		return true
	}

	matchedTokens := 0
	for _, token := range tokens {
		if len(token) < 3 {
			continue
		}
		if strings.Contains(artist, token) {
			matchedTokens++
		}
	}
	if matchedTokens >= 1 {
		return true
	}

	if seed == "" {
		return false
	}
	if (strings.Contains(title, "feat") || strings.Contains(title, "with")) && strings.Contains(title, seed) {
		return true
	}

	return false
}

func minDesiredResultCount(maxResults int) int {
	if maxResults <= 0 {
		return 0
	}
	if maxResults < 20 {
		return maxResults / 2
	}
	if maxResults > 30 {
		return 20
	}
	return maxResults
}

func filterArtistCandidates(candidates []Candidate, artistQuery string) []Candidate {
	seed := normalizeArtistSeed(artistQuery)
	tokens := tokenize(seed)
	if len(tokens) == 0 {
		return candidates
	}

	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if artistModeRelevant(candidate.Result, seed, tokens) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}
