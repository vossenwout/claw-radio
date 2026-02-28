package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIWorkflowHasRequiredTriggersAndSteps(t *testing.T) {
	content, err := os.ReadFile(filepath.FromSlash(".github/workflows/ci.yml"))
	if err != nil {
		t.Fatalf("failed to read workflow: %v", err)
	}

	text := string(content)
	required := []string{
		"push:",
		"pull_request:",
		"branches:",
		"- main",
		"runs-on: ubuntu-latest",
		"actions/checkout@v4",
		"actions/setup-go@v5",
		"go-version-file: go.mod",
		"if [ -n \"$(gofmt -l .)\" ]; then",
		"go vet ./...",
		"go test ./...",
	}

	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("workflow missing required content %q", want)
		}
	}
}

func TestCIFormatCheckFailsForUnformattedGoFile(t *testing.T) {
	tmpDir := t.TempDir()

	unformatted := "package main\n\nfunc  main() {\nprintln(\"x\")\n}\n"
	file := filepath.Join(tmpDir, "bad.go")
	if err := os.WriteFile(file, []byte(unformatted), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cmd := exec.Command("sh", "-c", "if [ -n \"$(gofmt -l .)\" ]; then gofmt -l .; exit 1; fi")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected format check to fail for unformatted file")
	}
	if !strings.Contains(string(out), "bad.go") {
		t.Fatalf("expected output to mention bad.go, got: %s", string(out))
	}
}
