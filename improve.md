# Search Quality Improvement Plan (Major)

This document describes what needs to change to make `claw-radio search` return high-quality songs from both specific and generic queries (for example: "Kendrick Lamar songs"), with concrete code-level impact.

No implementation changes are included here yet.

Design principle: keep CLI behavior deterministic. The AI agent can generate queries; the CLI should parse/extract/rank predictably and expose explicit knobs.

## 1) What I tested and what it shows

I ran the current binary against live queries and inspected raw SearxNG responses.

### Reproduction highlights

- `bin/claw-radio search "Billboard Year-End Hot 100 2024 site:wikipedia.org"`
  - Current output: `[]`
  - But raw SearxNG results are mostly valid `en.wikipedia.org` pages.
- `bin/claw-radio search "Kendrick Lamar songs"`
  - Current output contains CSS/article boilerplate, not songs.
  - Examples returned as "songs":
    - `.dhHfCb{position:absolute;...}`
    - `Rolling Stone - View all posts`
    - `WordPress.com VIP - Powered`
- `bin/claw-radio search "kendrick lamager songs"` (typo)
  - Raw SearxNG still returns useful URLs (Genius, Variety, Spotify, YouTube).
  - Final extractor output is still mostly garbage.

### Key finding

The largest quality issue is **not just query wording**. The pipeline currently loses quality in three places:

1. **Fetch reliability** (blocked pages like Wikipedia due request headers)
2. **Extraction quality** (generic parser over-matches non-song text)
3. **Ranking/selection** (no confidence scoring; low-quality pairs survive)

## 2) Root causes in current code

### A. URL retrieval is too shallow and unstructured

File: `internal/search/search.go`

- `fetchURLs(query, 6)` only takes top 6 URLs.
- Uses one raw query only (deterministic, but limited recall for broad intents).
- Keeps only URL, discards search metadata (`title`, `content`, `engine`, `score`) that could help ranking.

Impact: one unlucky set of pages dominates output quality.

### B. Page fetching is easy to block

File: `internal/search/search.go`

- Uses `http.Get` without browser-like headers in `fetchAndExtract`.
- No retry/backoff policy.
- All per-page failures are silently skipped (`continue`), no reason tracking.

Impact: `site:wikipedia.org` can return 0 songs even when result URLs are correct.

### C. Generic extractor over-accepts junk

File: `internal/search/extract.go`

- `Generic()` strips HTML tags but not script/style semantics.
- Hyphen pattern accepts arbitrary text lines as `Artist - Title`.
- No strong rejection for CSS, JS, CMS boilerplate, nav/footer fragments.

Impact: many false positives from modern JS/CMS pages.

### D. Domain parser coverage is incomplete for common result domains

Files: `internal/search/search.go`, `internal/search/extract.go`

- Only domain-specific handlers today: Wikipedia, Discogs, MusicBrainz.
- Many common domains in live results are handled by `Generic()` (Billboard, Genius, Spotify, Apple, YouTube, Rolling Stone, Kworb, etc.).
- Discogs frequently returns 403 in this environment; current docs still recommend heavy Discogs usage.

Impact: extractor is forced to parse pages it is not designed for.

### E. No post-extraction confidence model

File: `internal/search/search.go`

- All extracted pairs are treated equally after dedupe.
- No source trust score, no cross-source agreement bonus, no penalties for suspicious fields.

Impact: garbage and real songs compete equally.

### F. UX and docs do not guide safe query usage

Files: `SKILL.md`, `README.md`

- Query examples rely on `site:discogs.com` and other brittle flows.
- No user-facing guidance on query classes that currently perform best/worst.

Impact: users naturally run queries that the current parser cannot handle reliably.

## 3) Quality goals (explicit)

To make progress measurable, define target metrics:

- **Precision@20 >= 0.75** for "artist songs" queries (at least 15 valid songs in top 20).
- **Chart extraction recall >= 0.85** on canonical chart pages (Wikipedia/Billboard fixtures).
- **Blocked-page resilience:** if top domain blocks, system still returns useful output via fallback URLs.
- **Noise rate <= 10%** (non-song strings in final list).

Use these as acceptance criteria for implementation phases.

## 4) Required architecture changes

## 4.1 Deterministic query profiles (optional, explicit)

Avoid opaque intent classification. If we add query expansion, make it deterministic and opt-in.

### New behavior

- Default behavior remains one raw query only.
- Add explicit `--mode` that maps to fixed profile templates:
  - `--mode=raw` (default): run query exactly as provided
  - `--mode=artist-top`: run fixed template set (e.g. 4 known subqueries)
  - `--mode=chart-year`: run fixed template set for chart/year retrieval
- Optional typo assist uses SearxNG suggestions only when `--expand-suggestions` is set.

### Code changes

- New file: `internal/search/query_profile.go`
  - `type QueryMode string`
  - `func BuildProfile(mode QueryMode, raw string) []string`
- Update `internal/search/search.go`
  - `SearchWithStats` should execute one query in `raw` mode, or profile queries in explicit non-default modes.

## 4.2 Better SearxNG retrieval model

Stop treating search API as URL-only.

### New behavior

- Parse and keep `url`, `title`, `content`, `score`, `engine`, `category` from SearxNG.
- Fetch more candidates (e.g. 20 per plan, not fixed 6).
- Canonicalize URLs (strip tracking params like `srsltid`, dedupe by normalized URL).
- Prefer domains with known parsers.

### Code changes

- Refactor `fetchURLs` into `fetchResults` in `internal/search/search.go`.
- Add search result type:
  - `type SearchHit struct { URL, Title, Snippet, Domain string; Score float64; ... }`

## 4.3 Fetch robustness and observability

### New behavior

- Use a shared HTTP request builder with headers:
  - `User-Agent`, `Accept`, `Accept-Language`, `Referer` (optional)
- Retry policy for transient errors (`429`, `5xx`, resets).
- Parallel page fetch with bounded worker pool.
- Track skip reasons per domain/status and expose in debug stats.

### Code changes

- `internal/search/search.go`
  - Add helper `newRequest(rawURL)`
  - Add `fetchPageWithRetry(...)`
  - Add worker pool in `SearchWithStats`
- `cmd/search.go`
  - Optional `--debug` to print:
    - pages attempted/succeeded/failed
    - top failure reasons
    - extracted candidates before/after ranking

## 4.4 Extractor overhaul

The extraction step must become "quality-first".

### Generic extractor hardening

File: `internal/search/extract.go`

Add strong pre-clean + rejection:

- Remove `<script>`, `<style>`, `<noscript>`, `<svg>`, `<template>` blocks before tag stripping.
- Reject lines that look like CSS/JS:
  - high symbol ratio (`{}`, `;`, `/*`, `=>`, `function(`)
  - class/id selector signatures (`.foo{`, `#id{`)
- Reject known boilerplate phrases (`powered by`, `view all posts`, `cookie`, `subscribe`, etc.).
- Strip trailing numeric counters from title fields (`..., 1,234,567 8,901`).
- Add normalization for prefixes/suffixes (`"- YouTube"`, `"| Spotify"`, etc.).

### Domain-specific extractors (high ROI)

Add dedicated extractors for domains that appear frequently in real results:

- `Billboard(...)` for list/chart pages
- `GeniusSongs(...)` for artist songs pages
- `Kworb(...)` (Spotify top songs page format)
- `YouTube(...)` title parser fallback for direct video pages
- Improve `MusicBrainz(...)` fallback artist extraction from `<title>` or page heading when `og:title` missing
- Improve `Wikitable(...)` header row detection beyond only first row

### Code changes

- `internal/search/extract.go`
  - add parser funcs + reusable helpers
- `internal/search/search.go`
  - route domains to new parsers

## 4.5 Candidate scoring, ranking, and filtering

After extraction, candidates need confidence-based ordering.

### New behavior

- Score each pair with weighted signals:
  - source trust (`wikipedia`/`billboard`/`musicbrainz` > generic forums/stores)
  - extraction confidence (domain parser > generic line parser)
  - repetition across independent pages/subqueries
  - lexical quality penalties (too many symbols/digits, boilerplate tokens)
- Keep only above threshold; sort by score desc.
- Return top `maxResults` after ranking.

### Code changes

- New file: `internal/search/rank.go`
  - `type Candidate struct { Result; SourceURL; Domain; Confidence float64; Signals ... }`
  - `func RankCandidates(...) []Candidate`
- `internal/search/search.go`
  - use ranked candidates before final formatting.

## 4.6 Query UX changes for popular artist discovery

For user intent like "most popular Kendrick songs", single-query raw mode can be fragile.

### New behavior

- Keep default deterministic behavior (`raw` mode).
- Add optional explicit profile mode:
  - `search --mode=artist-top "Kendrick Lamar"`
  - `search --mode=chart-year "Billboard Year-End Hot 100 2024"`
- Keep output shape backward-compatible (`[]string`) unless `--debug` requested.

### Code changes

- `cmd/search.go`
  - add `--mode`, `--debug`, maybe `--max-pages`, `--expand-suggestions`
- `internal/search/search.go`
  - accept mode/options struct

## 4.7 Config upgrades

Current config only has `search.searxng_url`.

### New config fields

File: `internal/config/config.go`, `config.example.json`

```json
"search": {
  "searxng_url": "http://localhost:8888",
  "max_search_hits": 20,
  "max_pages": 20,
  "fetch_concurrency": 6,
  "request_timeout_seconds": 30,
  "user_agent": "claw-radio/<version> (+https://github.com/vossenwout/claw-radio)",
  "enable_query_expansion": false,
  "debug": false
}
```

## 4.8 Documentation and skill updates

### SKILL changes

File: `SKILL.md`

- Replace brittle one-shot examples with "run 3-5 focused queries and merge" guidance.
- De-emphasize Discogs-heavy patterns unless parser/fetch reliability is improved.
- Add preferred templates for artist-popular intent:
  - `<artist> most popular songs`
  - `<artist> top songs billboard`
  - `<artist> songs wikipedia`
  - `<artist> spotify top songs`

### README changes

- Clarify that search quality depends on source quality and extractor confidence.
- Document `--debug` output and best practices.

## 5) Test plan required for rollout

Search quality work needs stronger tests than pure unit coverage.

## 5.1 Unit tests

- `internal/search/query_profile_test.go`
  - deterministic mode->query template mapping
- `internal/search/extract_generic_test.go`
  - CSS/JS/boilerplate rejection fixtures
- `internal/search/rank_test.go`
  - scoring and threshold behavior

## 5.2 Parser fixture tests

- Add fixtures under `internal/search/testdata/` for:
  - billboard list page snapshot
  - genius songs page snapshot
  - kworb songs snapshot
  - noisy CMS page snapshot
- Assert precision-oriented expected outputs.

## 5.3 Client integration tests

File: `internal/search/search_client_test.go`

- Add header-sensitive server test:
  - server returns 403 unless `User-Agent` is present
- Add retry behavior tests.
- Add ranking regression tests (good source should outrank garbage line).

## 5.4 End-to-end CLI behavior tests

File: `cmd/search_test.go`

- Ensure backward-compatible JSON output in normal mode.
- Verify `--debug` summary includes failure reasons + counts.

## 5.5 Manual quality evaluation harness

Add a script (or Go test helper) that runs a fixed query suite and measures rough precision.

Suggested suite:

- `Kendrick Lamar songs`
- `Kendrick Lamar most popular songs`
- `Billboard Year-End Hot 100 2024`
- `Billboard Year-End Hot 100 2024 site:wikipedia.org`
- `Daft Punk top songs`

Track before/after for:

- valid songs in top 20
- garbage lines in top 20
- unique good songs total

## 6) Recommended implementation order (lowest risk -> highest impact)

## Phase 1: Reliability + anti-garbage baseline (must do first)

- Add headers/retries/observability in `search.go`
- Harden `Generic()` against CSS/boilerplate
- Increase fetch depth from 6 to configurable limit

Expected: immediate major drop in junk output and Wikipedia failures.

## Phase 2: Ranking layer

- Add candidate scoring + thresholding
- Add source trust model

Expected: garbage candidates demoted even when extracted.

## Phase 3: Optional deterministic profile modes

- Add explicit mode-based profile expansion (`raw`, `artist-top`, `chart-year`)
- Add optional suggestions expansion behind explicit flag
- Add `--mode` UX

Expected: better recall for generic artist queries.

## Phase 4: Domain parser expansion

- Add Billboard/Genius/Kworb/YouTube special extractors
- Improve MusicBrainz/Wikipedia robustness

Expected: significant precision jump on popular real-world sources.

## 7) Non-goals / cautions

- Do not rely on Discogs as a primary source unless fetch reliability is demonstrably stable in your runtime environment.
- Do not switch output format by default; keep existing `[]string` compatibility.
- Avoid overfitting to one artist; keep fixtures multi-genre/multi-era.

## 8) Bottom line

To make generic queries good, this tool needs to evolve from:

"single query + fetch 6 pages + regex hyphen parser"

to:

"deterministic retrieval (raw or explicit profile mode) + robust fetch + source-specific extraction + confidence ranking."

That combination is what will make queries like "most popular Kendrick songs" consistently useful instead of noisy.
