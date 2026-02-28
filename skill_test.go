package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readSkill(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.FromSlash("SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}

	return string(data)
}

func sectionBody(t *testing.T, doc, heading string) string {
	t.Helper()

	start := strings.Index(doc, heading)
	if start == -1 {
		t.Fatalf("missing heading %q", heading)
	}

	bodyStart := start + len(heading)
	rest := doc[bodyStart:]
	next := strings.Index(rest, "\n## ")
	if next == -1 {
		return strings.TrimSpace(rest)
	}

	return strings.TrimSpace(rest[:next])
}

func TestSkillFrontmatterHasNameAndDescriptionCoverage(t *testing.T) {
	skill := readSkill(t)

	if !strings.HasPrefix(skill, "---\n") {
		t.Fatal("SKILL.md must start with YAML frontmatter")
	}

	parts := strings.SplitN(skill, "\n---\n", 2)
	if len(parts) != 2 {
		t.Fatal("SKILL.md frontmatter must be closed by '---'")
	}

	frontmatter := strings.TrimPrefix(parts[0], "---\n")

	if ok, err := regexp.MatchString(`(?m)^name:\s*claw-radio\s*$`, frontmatter); err != nil {
		t.Fatalf("invalid name regex: %v", err)
	} else if !ok {
		t.Fatal("frontmatter must include name: claw-radio")
	}

	requiredDescriptionContent := []string{
		"start the radio",
		"build a song seed list",
		"inject spoken banter",
		"react to playback events",
	}
	for _, want := range requiredDescriptionContent {
		if !strings.Contains(frontmatter, want) {
			t.Fatalf("frontmatter description missing %q", want)
		}
	}
}

func TestSkillPersonaListsAtLeastSixGenreVoiceStyles(t *testing.T) {
	skill := readSkill(t)
	persona := sectionBody(t, skill, "## Persona")

	styles := regexp.MustCompile(`(?m)^- \*\*[^*]+\*\*:`).FindAllString(persona, -1)
	if len(styles) < 6 {
		t.Fatalf("expected at least 6 persona voice styles, got %d", len(styles))
	}
}

func TestSkillSeedBuildingIncludesAllThreeVibeTypesWithQueries(t *testing.T) {
	skill := readSkill(t)

	sections := []string{
		"### Era / genre vibes",
		"### Artist-based vibes",
		"### Mood / abstract vibes",
	}

	for _, heading := range sections {
		section := sectionBody(t, skill, heading)
		if !strings.Contains(section, "claw-radio search") {
			t.Fatalf("%s must include at least one query example", heading)
		}
	}
}

func TestSkillStartupFlowContainsFullCommandSequenceEndingInEventsLoop(t *testing.T) {
	skill := readSkill(t)
	startup := sectionBody(t, skill, "## Full startup flow")

	required := []string{
		"claw-radio start",
		"claw-radio search",
		"claw-radio seed",
		"claw-radio events --json | while read -r event; do",
		"done",
	}
	for _, want := range required {
		if !strings.Contains(startup, want) {
			t.Fatalf("startup flow missing %q", want)
		}
	}
}

func TestSkillTrackStartedHandlerRequiresBanterBeforeEverySong(t *testing.T) {
	skill := readSkill(t)
	events := sectionBody(t, skill, "## Reacting to events")

	required := []string{
		"`track_started`",
		"before every song",
		"Always say something",
		"claw-radio say",
	}
	for _, want := range required {
		if !strings.Contains(events, want) {
			t.Fatalf("track_started handler missing %q", want)
		}
	}
}
