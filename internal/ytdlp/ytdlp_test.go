package ytdlp

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestScoreAcceptanceCriteria(t *testing.T) {
	t.Run("official audio is strongly positive", func(t *testing.T) {
		got := Score(Candidate{Title: "Song (Official Audio)", Duration: 200})
		if got < 40 {
			t.Fatalf("Score() = %d, want >= 40", got)
		}
	})

	t.Run("is_live gets heavy penalty", func(t *testing.T) {
		got := Score(Candidate{Title: "Song Live at VMAs", IsLive: true})
		if got > -100 {
			t.Fatalf("Score() = %d, want <= -100", got)
		}
	})

	t.Run("short clip applies -80 penalty", func(t *testing.T) {
		got := Score(Candidate{Duration: 60})
		if got != -80 {
			t.Fatalf("Score() = %d, want -80", got)
		}
	})

	t.Run("vevo and high views add bonuses", func(t *testing.T) {
		got := Score(Candidate{
			Uploader:  "ArtistVEVO",
			ViewCount: 10_000_000,
			Duration:  210,
		})
		if got < 59 {
			t.Fatalf("Score() = %d, want at least 59 (VEVO + 5M tier)", got)
		}
	})

	t.Run("multiple negative keywords stack", func(t *testing.T) {
		base := Score(Candidate{Title: "Song", Duration: 200})
		got := Score(Candidate{Title: "Song - lyrics nightcore cover", Duration: 200})
		if base-got < 90 {
			t.Fatalf("Score delta = %d, want at least 90 for three keyword penalties", base-got)
		}
	})
}

func TestBestCandidateReturnsHighestScoringCandidate(t *testing.T) {
	setMockExecCommand(t)
	t.Setenv("YTDLP_HELPER_EXPECT_CMD", "yt-dlp-bin")
	t.Setenv("YTDLP_HELPER_EXPECT_ARGS", strings.Join([]string{
		"--dump-json",
		"ytsearch10:best song",
	}, "\x1f"))
	t.Setenv("YTDLP_HELPER_STDOUT", strings.Join([]string{
		`{"id":"low","title":"Song Live","is_live":true,"duration":200}`,
		`{"id":"mid","title":"Song (Official Audio)","duration":200}`,
		`{"id":"high","title":"Song","uploader":"ArtistVEVO","view_count":10000000,"duration":210}`,
	}, "\n"))

	got, err := BestCandidate("yt-dlp-bin", "best song")
	if err != nil {
		t.Fatalf("BestCandidate() error: %v", err)
	}
	if got == nil {
		t.Fatalf("BestCandidate() returned nil candidate")
	}
	if got.ID != "high" {
		t.Fatalf("BestCandidate().ID = %q, want %q", got.ID, "high")
	}
}

func TestNormalizeSongKey(t *testing.T) {
	got := NormalizeSongKey("Britney Spears - Oops! I Did It Again (Official Audio)")
	want := "britney spears - oops i did it again"
	if got != want {
		t.Fatalf("NormalizeSongKey() = %q, want %q", got, want)
	}
}

func setMockExecCommand(t *testing.T) {
	t.Helper()

	orig := execCommand
	execCommand = helperCommand
	t.Cleanup(func() {
		execCommand = orig
	})

	t.Setenv("YTDLP_HELPER_EXPECT_CMD", "")
	t.Setenv("YTDLP_HELPER_EXPECT_ARGS", "")
	t.Setenv("YTDLP_HELPER_STDOUT", "")
	t.Setenv("YTDLP_HELPER_STDERR", "")
	t.Setenv("YTDLP_HELPER_EXIT", "0")
}

func helperCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestYtDlpHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_YTDLP_HELPER=1")
	return cmd
}

func TestYtDlpHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_YTDLP_HELPER") != "1" {
		return
	}

	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+1 >= len(args) {
		fmt.Fprint(os.Stderr, "missing helper separator")
		os.Exit(2)
	}

	gotCmd := args[sep+1]
	gotArgs := args[sep+2:]

	if wantCmd := os.Getenv("YTDLP_HELPER_EXPECT_CMD"); wantCmd != "" && gotCmd != wantCmd {
		fmt.Fprintf(os.Stderr, "unexpected command: got %q want %q", gotCmd, wantCmd)
		os.Exit(3)
	}

	if wantArgsRaw := os.Getenv("YTDLP_HELPER_EXPECT_ARGS"); wantArgsRaw != "" {
		wantArgs := strings.Split(wantArgsRaw, "\x1f")
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			fmt.Fprintf(os.Stderr, "unexpected args: got %#v want %#v", gotArgs, wantArgs)
			os.Exit(4)
		}
	}

	fmt.Fprint(os.Stdout, os.Getenv("YTDLP_HELPER_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("YTDLP_HELPER_STDERR"))

	code := 0
	if raw := os.Getenv("YTDLP_HELPER_EXIT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad YTDLP_HELPER_EXIT: %q", raw)
			os.Exit(5)
		}
		code = parsed
	}

	os.Exit(code)
}
