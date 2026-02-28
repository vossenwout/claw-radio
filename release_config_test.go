package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoReleaserConfigHasRequiredReleaseSettings(t *testing.T) {
	content, err := os.ReadFile(filepath.FromSlash(".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("failed to read .goreleaser.yaml: %v", err)
	}

	text := string(content)
	required := []string{
		"version: 2",
		"project_name: claw-radio",
		"CGO_ENABLED=0",
		"- darwin_amd64",
		"- darwin_arm64",
		"- linux_amd64",
		"- linux_arm64",
		"- -s -w -X main.version={{.Version}}",
		"universal_binaries:",
		"replace: true",
		"config.example.json",
		"README.md",
		"homebrew_casks:",
		"name: claw-radio-cli",
		"binaries:",
		"- claw-radio",
		"owner: vossenwout",
		"name: homebrew-tap",
		"token: \"{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}\"",
		"/usr/bin/xattr",
		"com.apple.quarantine",
	}

	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf(".goreleaser.yaml missing required content %q", want)
		}
	}
}

func TestReleaseWorkflowHasTagTriggerAndGoReleaserAction(t *testing.T) {
	content, err := os.ReadFile(filepath.FromSlash(".github/workflows/release.yml"))
	if err != nil {
		t.Fatalf("failed to read release workflow: %v", err)
	}

	text := string(content)
	required := []string{
		"name: Release",
		"push:",
		"tags:",
		"v*",
		"runs-on: ubuntu-latest",
		"actions/checkout@v4",
		"fetch-depth: 0",
		"actions/setup-go@v5",
		"goreleaser/goreleaser-action@v6",
		"args: release --clean",
		"GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		"HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}",
	}

	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("release workflow missing required content %q", want)
		}
	}
}

func TestGoReleaserConfigPassesCheckWhenInstalled(t *testing.T) {
	bin, err := exec.LookPath("goreleaser")
	if err != nil {
		t.Skip("goreleaser not installed; skipping local goreleaser check")
	}

	cmd := exec.Command(bin, "check", "--config", ".goreleaser.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser check failed: %v\n%s", err, string(out))
	}
}
