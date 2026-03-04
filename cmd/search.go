package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	searchpkg "github.com/vossenwout/claw-radio/internal/search"
)

type searchClient interface {
	SearchDetailed(query string, maxResults int, options searchpkg.SearchOptions) ([]searchpkg.Result, searchpkg.Stats, error)
}

var newSearchClientFn = func(searxngURL string, options ...searchpkg.ClientOption) searchClient {
	return searchpkg.NewClient(searxngURL, options...)
}

var (
	searchModeFlag              string
	searchDebugFlag             bool
	searchExpandSuggestionsFlag bool
	searchMaxPagesFlag          int
	searchEnginesFlag           []string
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Find song candidates from the web for your playlist",
	Long:  "Use this when you need more songs. It returns ranked \"Artist - Title\" candidates you can pass to playlist add.",
	Example: strings.Join([]string{
		`  claw-radio search "best 2000s pop songs" --mode genre-top`,
		`  claw-radio search "Kendrick Lamar" --mode artist-top`,
		`  claw-radio search "Taylor Swift 2014" --mode artist-year`,
		`  claw-radio search "Billboard Year-End Hot 100 2012" --mode chart-year`,
		`  claw-radio search "late-night R&B vibes" --mode chart-year,genre-top`,
	}, "\n"),
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return nil
		}
		if len(args) == 0 {
			return fmt.Errorf("accepts 1 arg(s), received 0: missing query. Example: claw-radio search \"best 90s hip hop\" --mode genre-top")
		}
		return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(cmd, args[0])
	},
}

func runSearch(cmd *cobra.Command, query string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	modes, err := parseSearchModes(searchModeFlag)
	if err != nil {
		return exitCode(err, 2)
	}

	client := newSearchClientFn(
		cfg.Search.SearxNGURL,
		searchpkg.WithMaxSearchHits(cfg.Search.MaxSearchHits),
		searchpkg.WithMaxPages(cfg.Search.MaxPages),
		searchpkg.WithFetchConcurrency(cfg.Search.FetchConcurrency),
		searchpkg.WithRequestTimeout(time.Duration(cfg.Search.RequestTimeoutSeconds)*time.Second),
		searchpkg.WithUserAgent(cfg.Search.UserAgent),
		searchpkg.WithEnableQueryExpansion(cfg.Search.EnableQueryExpansion),
	)

	options := searchpkg.SearchOptions{
		Mode:              modes[0],
		Modes:             modes,
		ExpandSuggestions: searchExpandSuggestionsFlag,
		MaxPages:          searchMaxPagesFlag,
		Engines:           resolveSearchEngines(cfg.Search, modes, searchEnginesFlag),
	}

	results, stats, err := client.SearchDetailed(query, 150, options)
	if err != nil {
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "searxng unreachable") {
			return exitCode(fmt.Errorf("could not reach SearxNG at %s\n- Check: is SearxNG running?\n- Check: search.searxng_url in config\n- Quick test: curl %s/search?q=test&format=json", cfg.Search.SearxNGURL, cfg.Search.SearxNGURL), 1)
		}
		return exitCode(err, 1)
	}

	formatted := make([]string, 0, len(results))
	for _, result := range results {
		formatted = append(formatted, fmt.Sprintf("%s - %s", result.Artist, result.Title))
	}

	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(formatted); err != nil {
		return fmt.Errorf("encode search results: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Found %d song candidates from %d pages.\n", len(formatted), stats.PagesAttempted)
	if len(formatted) == 0 && len(stats.UnresponsiveEngines) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "No search hits returned by SearxNG. Unresponsive engines: %s\n", strings.Join(stats.UnresponsiveEngines, "; "))
	}
	if searchDebugFlag || cfg.Search.Debug {
		printSearchDebugStats(cmd, stats)
	}

	return nil
}

func printSearchDebugStats(cmd *cobra.Command, stats searchpkg.Stats) {
	if len(stats.RequestedEngines) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Engines: %s\n", strings.Join(stats.RequestedEngines, ","))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Queries: %s\n", strings.Join(stats.Queries, " | "))
	fmt.Fprintf(cmd.ErrOrStderr(), "Pages succeeded: %d, failed: %d\n", stats.PagesFetched, stats.PagesFailed)
	fmt.Fprintf(cmd.ErrOrStderr(), "Candidates before ranking: %d, after ranking: %d\n", stats.CandidatesBeforeRank, stats.CandidatesAfterRank)

	if len(stats.FailureReasons) > 0 {
		keys := make([]string, 0, len(stats.FailureReasons))
		for reason := range stats.FailureReasons {
			keys = append(keys, reason)
		}
		sort.Strings(keys)

		for _, reason := range keys {
			fmt.Fprintf(cmd.ErrOrStderr(), "Skip reason %q: %d\n", reason, stats.FailureReasons[reason])
		}
	}

	if len(stats.UnresponsiveEngines) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Unresponsive engines: %s\n", strings.Join(stats.UnresponsiveEngines, "; "))
	}
}

func parseSearchModes(raw string) ([]searchpkg.QueryMode, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	modes := make([]searchpkg.QueryMode, 0, len(parts))
	seen := map[searchpkg.QueryMode]struct{}{}

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		mode := searchpkg.ParseMode(trimmed)
		if mode == "" {
			return nil, fmt.Errorf("invalid mode %q. Use one of: raw, artist-top, artist-year, chart-year, genre-top. Combine modes with commas, for example: --mode chart-year,genre-top", strings.TrimSpace(raw))
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		modes = append(modes, mode)
	}

	if len(modes) == 0 {
		return []searchpkg.QueryMode{searchpkg.ModeRaw}, nil
	}

	return modes, nil
}

func resolveSearchEngines(cfg config.SearchConfig, modes []searchpkg.QueryMode, flagValues []string) []string {
	if normalized := searchpkg.NormalizeEngineList(flagValues); len(normalized) > 0 {
		return normalized
	}

	combined := make([]string, 0)
	for _, mode := range modes {
		switch mode {
		case searchpkg.ModeArtistTop:
			combined = append(combined, cfg.ModeEngines.ArtistTop...)
		case searchpkg.ModeArtistYear:
			combined = append(combined, cfg.ModeEngines.ArtistYear...)
		case searchpkg.ModeChartYear:
			combined = append(combined, cfg.ModeEngines.ChartYear...)
		case searchpkg.ModeGenreTop:
			combined = append(combined, cfg.ModeEngines.GenreTop...)
		default:
			combined = append(combined, cfg.ModeEngines.Raw...)
		}
	}
	if normalized := searchpkg.NormalizeEngineList(combined); len(normalized) > 0 {
		return normalized
	}

	return searchpkg.NormalizeEngineList(cfg.Engines)
}

func init() {
	searchCmd.Flags().StringVar(&searchModeFlag, "mode", string(searchpkg.ModeRaw), "Search mode: raw, artist-top, artist-year, chart-year, genre-top (combine with commas)")
	searchCmd.Flags().BoolVar(&searchDebugFlag, "debug", false, "Print detailed search diagnostics to stderr")
	searchCmd.Flags().BoolVar(&searchExpandSuggestionsFlag, "expand-suggestions", false, "Expand query with one SearxNG suggestion")
	searchCmd.Flags().IntVar(&searchMaxPagesFlag, "max-pages", 0, "Override max pages fetched for this command")
	searchCmd.Flags().StringSliceVar(&searchEnginesFlag, "engines", nil, "Override SearxNG engines for this command (comma-separated)")
	RootCmd.AddCommand(searchCmd)
}
