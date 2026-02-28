package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	searchpkg "github.com/vossenwout/claw-radio/internal/search"
)

type searchClient interface {
	SearchWithStats(query string, maxResults int) ([]searchpkg.Result, int, error)
}

var newSearchClientFn = func(searxngURL string) searchClient {
	return searchpkg.NewClient(searxngURL)
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search pages and extract Artist - Title pairs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(cmd, args[0])
	},
}

func runSearch(cmd *cobra.Command, query string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	results, pagesFetched, err := newSearchClientFn(cfg.Search.SearxNGURL).SearchWithStats(query, 150)
	if err != nil {
		return exitCode(err, 1)
	}

	formatted := make([]string, 0, len(results))
	for _, result := range results {
		formatted = append(formatted, fmt.Sprintf("%s - %s", result.Artist, result.Title))
	}

	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(formatted); err != nil {
		return fmt.Errorf("encode search results: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Fetched %d pages, extracted %d unique songs.\n", pagesFetched, len(formatted))
	return nil
}

func init() {
	RootCmd.AddCommand(searchCmd)
}
