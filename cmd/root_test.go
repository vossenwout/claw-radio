package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestExecuteNoArgsPrintsHelp(t *testing.T) {
	SetVersion("dev")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs([]string{})

	if err := Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("help output must contain Usage:, got %q", output)
	}
}

func TestExecuteVersionPrintsVersionString(t *testing.T) {
	SetVersion("dev")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs([]string{"version"})

	if err := Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	got := strings.TrimSpace(stdout.String())
	want := "claw-radio dev"
	if got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestExecuteUnknownCommandReturnsUsageExitCode(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs([]string{"foobar"})

	err := Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want usage error")
	}

	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("Execute() error type = %T, want *exitError", err)
	}
	if ee.code != 2 {
		t.Fatalf("exit code = %d, want 2", ee.code)
	}
}

func TestExitCodeHelperPreservesCode(t *testing.T) {
	err := exitCode(errors.New("boom"), 5)
	if err.code != 5 {
		t.Fatalf("exitCode(..., 5).code = %d, want 5", err.code)
	}
}
