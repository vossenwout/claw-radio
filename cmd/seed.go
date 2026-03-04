package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/station"
)

var (
	seedAppendFlag bool
)

var seedCmd = &cobra.Command{
	Use:   "seed <json-array>",
	Short: "Set or append the station seed list",
	Long:  "Set or append the station seed list using a JSON string array of 'Artist - Title' entries.",
	Example: strings.Join([]string{
		"  claw-radio seed '[\"Britney Spears - Oops! I Did It Again\",\"NSYNC - Bye Bye Bye\"]'",
		"  claw-radio seed '[\"Daft Punk - One More Time\"]' --append",
	}, "\n"),
	Args: seedArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSeed(cmd, args[0])
	},
}

func seedArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return nil
	}
	_ = cmd.Help()
	return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
}

func runSeed(cmd *cobra.Command, raw string) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	seeds, err := parseSeeds(raw)
	if err != nil {
		return exitCode(err, 1)
	}

	st, err := station.Load(cfg.Station.StateDir)
	if err != nil {
		return fmt.Errorf("load station state: %w", err)
	}

	if seedAppendFlag {
		before := len(st.Seeds)
		st.AppendSeeds(seeds)
		added := len(st.Seeds) - before
		if err := st.Save(); err != nil {
			return fmt.Errorf("save station state: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Added %d songs (total: %d)\n", added, len(st.Seeds))
		return nil
	}

	st.SetSeeds(seeds, "")
	if err := st.Save(); err != nil {
		return fmt.Errorf("save station state: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Seeded %d songs\n", len(seeds))
	return nil
}

func parseSeeds(raw string) ([]string, error) {
	var seeds []string
	if err := json.Unmarshal([]byte(raw), &seeds); err != nil {
		return nil, fmt.Errorf("parse seed json: %w", err)
	}
	return seeds, nil
}

func init() {
	seedCmd.Flags().BoolVar(&seedAppendFlag, "append", false, "Append to existing seeds instead of replacing")
	RootCmd.AddCommand(seedCmd)
}
