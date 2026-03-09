package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type exitError struct {
	err  error
	code int
}

func (e *exitError) Error() string {
	return e.err.Error()
}

func (e *exitError) ExitCode() int {
	return e.code
}

func exitCode(err error, code int) *exitError {
	if err == nil {
		err = errors.New("unknown error")
	}
	return &exitError{err: err, code: code}
}

func NewExitError(err error, code int) error {
	return exitCode(err, code)
}

var version = "dev"

var RootCmd = &cobra.Command{
	Use:   "claw-radio",
	Short: "AI-operated GTA-style radio station CLI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print claw-radio version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "claw-radio %s\n", version)
	},
}

func Execute() error {
	err := RootCmd.Execute()
	if err == nil {
		return nil
	}

	if isUsageError(err) {
		return exitCode(err, 2)
	}

	return err
}

func SetVersion(v string) {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		version = "dev"
		return
	}
	version = trimmed
}

func isUsageError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "requires at least") ||
		strings.Contains(msg, "accepts ") ||
		strings.Contains(msg, "required flag")
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
