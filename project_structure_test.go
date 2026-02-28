package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredDirectoriesExist(t *testing.T) {
	t.Parallel()

	dirs := []string{
		"cmd",
		"internal/config",
		"internal/mpv",
		"internal/ytdlp",
		"internal/station",
		"internal/tts",
		"internal/search",
		"internal/provider",
		"tts",
		"tts/voices",
		".github/workflows",
	}

	for _, dir := range dirs {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			info, err := os.Stat(filepath.FromSlash(dir))
			if err != nil {
				t.Fatalf("expected %s to exist: %v", dir, err)
			}
			if !info.IsDir() {
				t.Fatalf("expected %s to be a directory", dir)
			}
		})
	}
}

func TestMainGoStubExists(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("expected main.go to exist: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "package main") {
		t.Fatalf("main.go must contain package main")
	}
	if !strings.Contains(content, "func main() {}") {
		t.Fatalf("main.go must contain stub func main() {}")
	}
}

func TestGoModContainsModuleAndCobra(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("expected go.mod to exist: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "module github.com/vossenwout/claw-radio") {
		t.Fatalf("go.mod must contain module github.com/vossenwout/claw-radio")
	}
	if !strings.Contains(content, "github.com/spf13/cobra") {
		t.Fatalf("go.mod must contain github.com/spf13/cobra dependency")
	}
}
