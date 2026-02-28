package search

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	searxngURL string
	httpClient *http.Client
}

func NewClient(searxngURL string) *Client {
	trimmed := strings.TrimRight(strings.TrimSpace(searxngURL), "/")
	return &Client{
		searxngURL: trimmed,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Search(query string, maxResults int) ([]Result, error) {
	if maxResults <= 0 {
		return []Result{}, nil
	}

	urls, err := c.fetchURLs(query, 6)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, maxResults)
	seen := make(map[string]struct{})

	for _, rawURL := range urls {
		extracted, err := c.fetchAndExtract(rawURL)
		if err != nil {
			continue
		}

		for _, r := range extracted {
			artist := strings.TrimSpace(r.Artist)
			title := strings.TrimSpace(r.Title)
			if artist == "" || title == "" {
				continue
			}

			key := strings.ToLower(artist + "||" + title)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, Result{Artist: artist, Title: title})
			if len(results) >= maxResults {
				return results[:maxResults], nil
			}
		}
	}

	return results, nil
}

func (c *Client) fetchURLs(query string, n int) ([]string, error) {
	if n <= 0 {
		return []string{}, nil
	}

	base := strings.TrimRight(c.searxngURL, "/")
	endpoint := base + "/search?q=" + url.QueryEscape(strings.TrimSpace(query)) + "&format=json"

	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("searxng unreachable at %s: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng unreachable at %s: status %d", base, resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse searxng response from %s: %w", endpoint, err)
	}

	urls := make([]string, 0, n)
	for _, r := range payload.Results {
		rawURL := strings.TrimSpace(r.URL)
		if rawURL == "" {
			continue
		}
		urls = append(urls, rawURL)
		if len(urls) >= n {
			break
		}
	}

	return urls, nil
}

func (c *Client) fetchAndExtract(rawURL string) ([]Result, error) {
	resp, err := c.httpClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	html := string(body)

	lowerURL := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lowerURL, "wikipedia.org"):
		return Wikitable(html), nil
	case strings.Contains(lowerURL, "discogs.com"):
		return Discogs(html), nil
	case strings.Contains(lowerURL, "musicbrainz.org"):
		return MusicBrainz(html), nil
	default:
		return Generic(html), nil
	}
}
