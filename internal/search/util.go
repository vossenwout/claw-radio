package search

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	nonContentBlockRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>|<noscript\b[^>]*>.*?</noscript>|<svg\b[^>]*>.*?</svg>|<template\b[^>]*>.*?</template>`)
	cssLikeLineRe     = regexp.MustCompile(`(?i)(^\s*[.#][a-z0-9_-]+\s*\{|/\*|\*/|\{\s*[^}]*:\s*[^}]*\}|function\s*\(|=>|var\s+|let\s+|const\s+)`)
)

var boilerplatePhrases = []string{
	"wordPress.com vip",
	"view all posts",
	"more stories",
	"powered",
	"apple music",
	"spotify chart history",
	"chart history",
	"official store",
	"story behind",
	"illustration",
	"billboard ranks",
	"ranks the tracks",
	"final thoughts",
	"all rights administered",
	"song list",
	"trusted",
	"music & licensing",
	"subscribe",
	"cookie",
	"privacy policy",
	"terms of use",
	"all rights reserved",
	"javascript",
}

func stripNonContentBlocks(s string) string {
	return nonContentBlockRe.ReplaceAllString(s, " ")
}

func looksLikeCodeLine(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if cssLikeLineRe.MatchString(trimmed) {
		return true
	}
	if strings.Count(trimmed, "{")+strings.Count(trimmed, "}") >= 2 {
		return true
	}
	if strings.Count(trimmed, ";") >= 2 {
		return true
	}
	return false
}

func looksLikeBoilerplate(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	for _, phrase := range boilerplatePhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func canonicalDomain(raw string) string {
	domain := strings.TrimSpace(strings.ToLower(raw))
	if strings.HasPrefix(domain, "www.") {
		domain = strings.TrimPrefix(domain, "www.")
	}
	return domain
}

func canonicalDomainFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return canonicalDomain(parsed.Hostname())
}
