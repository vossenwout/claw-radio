package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readREADME(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.FromSlash("README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}

	return string(data)
}

func TestREADMEInstallationIncludesMacOSAndLinuxCommands(t *testing.T) {
	readme := readREADME(t)

	if !strings.Contains(readme, "brew install vossenwout/tap/claw-radio-cli") {
		t.Fatal("README missing Homebrew install command")
	}
	if !strings.Contains(readme, "curl -fsSL") || !strings.Contains(readme, "tar -xzf") {
		t.Fatal("README missing Linux curl + tar install commands")
	}
}

func TestREADMEDependencySectionIncludesRequiredTools(t *testing.T) {
	readme := readREADME(t)

	if !strings.Contains(readme, "brew install mpv yt-dlp ffmpeg") {
		t.Fatal("README missing macOS dependency install command for mpv, yt-dlp, and ffmpeg")
	}
	if !strings.Contains(readme, "sudo apt install -y mpv yt-dlp ffmpeg") {
		t.Fatal("README missing Linux dependency install command for mpv, yt-dlp, and ffmpeg")
	}
}

func TestREADMECLIReferenceListsCommandsWithFlagsAndDescriptions(t *testing.T) {
	readme := readREADME(t)

	if !strings.Contains(readme, "## CLI Reference") {
		t.Fatal("README missing CLI Reference section")
	}

	requiredCommands := []string{
		"tts install",
		"tts voice add",
		"start",
		"stop",
		"play",
		"queue",
		"pause",
		"resume",
		"next",
		"seed",
		"search",
		"say",
		"events",
		"status",
		"version",
	}

	for _, cmd := range requiredCommands {
		pattern := `(?m)^\| ` + regexp.QuoteMeta("`"+cmd+"`") + ` \| ` + "`[^`]+`" + ` \| [^|\n]+ \|$`
		if ok, err := regexp.MatchString(pattern, readme); err != nil {
			t.Fatalf("invalid regex for command %q: %v", cmd, err)
		} else if !ok {
			t.Fatalf("README missing CLI row for %q with flags and one-line description", cmd)
		}
	}
}
