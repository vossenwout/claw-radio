package main

import (
	"os"

	"github.com/vossenwout/claw-radio/cmd"
)

var version = "dev"
var execute = cmd.Execute
var osExit = os.Exit
var osArgs = os.Args

func main() {
	cmd.SetVersion(version)
	cmd.SetEmbeddedTTSFS(ttsFS)
	os.Args = osArgs

	err := execute()
	if err == nil {
		return
	}

	if exitErr, ok := err.(interface{ ExitCode() int }); ok {
		osExit(exitErr.ExitCode())
	}

	osExit(1)
}
