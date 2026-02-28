package cmd

import "github.com/spf13/cobra"

var RootCmd = &cobra.Command{
	Use: "claw-radio",
}

func Execute() error {
	return RootCmd.Execute()
}
