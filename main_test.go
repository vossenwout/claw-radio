package main

import (
	"errors"
	"testing"

	"github.com/vossenwout/claw-radio/cmd"
)

type exitPanic struct {
	code int
}

func TestMainExitsWithCommandExitCode(t *testing.T) {
	originalArgs := osArgs
	originalExit := osExit
	originalExecute := execute
	originalVersion := version
	defer func() {
		osArgs = originalArgs
		osExit = originalExit
		execute = originalExecute
		version = originalVersion
	}()

	osArgs = []string{"claw-radio"}
	version = "dev"
	execute = func() error {
		return cmd.NewExitError(errors.New("forced"), 5)
	}

	gotCode := -1
	osExit = func(code int) {
		gotCode = code
		panic(exitPanic{code: code})
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("main() did not exit")
		}
		if _, ok := recovered.(exitPanic); !ok {
			t.Fatalf("main() panic = %v, want exit panic", recovered)
		}
		if gotCode != 5 {
			t.Fatalf("main() exit code = %d, want 5", gotCode)
		}
	}()

	main()
}
