# Implementation tasks

Implementation tasks for building the specs outlined in the design doc: [plan](plan.md).

**Task Format**

## How to write a good task

- One task = one implementable unit of work.
- Numbering: use sequential integers (`T:1`, `T:2`, ...). Never reuse numbers.
- Title: verb + object + context (e.g. "Create a new canvas").
- Description: Clearly describe  the feature/behavior to implement (no personas/benefits).
- Acceptance criteria must be testable/observable; cover happy path + key error case(s). Ofcourse also write tests and run them to verify this before you check it off.

## Task Template

### T:$NUMBER$: $TITLE$

**Description**
$DESCRIBE_THE_FEATURE_TO_IMPLEMENT.
$INCLUDE_KEY_BEHAVIOR_AND_CONSTRAINTS (inputs/outputs, CLI command/flags, defaults).

**Acceptance criteria:**

- [ ] Given $CONTEXT, when $ACTION, then $EXPECTED_RESULT
- [ ] Error: given $BAD_INPUT, when $ACTION, then $ERROR_BEHAVIOR
- [ ] Output: the CLI prints/returns $EXPECTED_OUTPUT (example included if needed)

---

## Phase 1: Foundation

### T:1: Initialize Go module and project structure

**Description**
Create the Go module, directory layout, and package stubs as defined in plan.md
section 4. Module path is `github.com/vossenwout/claw-radio`. Add
`github.com/spf13/cobra` as the only external dependency. Create all directories
and empty `.go` files (package declarations only) so the module compiles from
day one.

Directories to create:
`cmd/`, `internal/config/`, `internal/mpv/`, `internal/ytdlp/`,
`internal/station/`, `internal/tts/`, `internal/search/`, `internal/provider/`,
`tts/`, `tts/voices/`, `.github/workflows/`

**Acceptance criteria:**

- [x] Given the repo root, when `go mod tidy` runs, then `go.mod` has module `github.com/vossenwout/claw-radio` and cobra listed as a dependency
- [x] Given the repo root, when `go build ./...` runs, then it succeeds with no errors
- [x] All directories listed above exist
- [x] `main.go` exists with `package main` and a stub `func main() {}`

---

### T:2: Implement config loader

**Description**
Implement `internal/config/config.go` and `internal/config/config_test.go` as
specified in plan.md section 5 and 7.1.

Config is loaded from `~/.config/claw-radio/config.json` by default, overridable
via the `CLAW_RADIO_CONFIG` env var. **No config file is not an error** — the tool
must work with zero configuration. Defaults:
- `mpv.socket` = `/tmp/claw-radio-mpv.sock`
- `mpv.log` = `~/.local/share/claw-radio/mpv.log`
- `tts.socket` = `/tmp/claw-radio-tts.sock`
- `tts.data_dir` = `~/.local/share/claw-radio`
- `station.queue_depth` = `5`
- `station.cache_dir` = `~/.local/share/claw-radio/cache`
- `station.state_dir` = `~/.local/share/claw-radio/state`
- `search.searxng_url` = `http://localhost:8888`

Binary fields (mpv.binary, ytdlp.binary, ffmpeg.binary, tts.fallback_binary) with
empty strings are resolved via `exec.LookPath`. `tts.fallback_binary` is
auto-detected: `"say"` on `runtime.GOOS == "darwin"`, else tries `"espeak-ng"`,
`"espeak"`, `"festival"` in order. Paths containing `~` are expanded to
`os.UserHomeDir()`. This is the only place `runtime.GOOS` appears.

**Acceptance criteria:**

- [x] Given no config file exists, when `Load()` is called, then it returns a fully populated struct with all defaults — no error
- [x] Given a valid config JSON at `CLAW_RADIO_CONFIG`, when `Load()` is called, then the struct fields override the defaults correctly
- [x] Given malformed JSON in the config file, when `Load()` is called, then it returns an error containing the file path
- [x] Given `mpv.binary` is empty in config, when `Load()` is called, then `cfg.MPV.Binary` is the resolved path from `exec.LookPath("mpv")` or empty string if not found
- [x] Given `tts.data_dir` contains `"~"`, when `Load()` is called, then the `~` is expanded to the real home directory path
- [x] On darwin, `Load()` resolves `cfg.TTS.FallbackBinary` to the path of `say`; on non-darwin it tries `espeak-ng`, `espeak`, `festival` in order (tests should be OS-conditional)
- [x] `go test ./internal/config/...` passes with no external tools installed

---

## Phase 2: Internal packages

### T:3: Implement mpv IPC client

**Description**
Implement `internal/mpv/client.go` and `internal/mpv/client_test.go` as specified
in plan.md section 7.2.

Direct Unix socket IPC client using `net.DialUnix`. The client runs a background
goroutine that reads from the socket and dispatches messages:
- Messages with a `request_id` field are responses — forwarded to the matching
  pending channel.
- Messages without `request_id` are events — sent to the `events` channel.

Multiple clients can connect to the same mpv socket simultaneously (e.g. the
controller and the `events` command) and each receive the full event stream.

Functions to implement:
- `Dial(socketPath string) (*Client, error)`
- `Close() error`
- `Command(args ...interface{}) error` — sends `{"command": args, "request_id": n}`, waits for response, returns error if mpv reports an error
- `Get(prop string) (json.RawMessage, error)` — sends `get_property` command
- `Set(prop string, value interface{}) error` — sends `set_property` command
- `LoadFile(path, mode string) error` — sends `loadfile` command
- `InsertNext(path string) error` — four-step sequence: LoadFile append → get playlist-pos → get playlist-count → if count > 1 and pos >= 0: playlist-move last item to pos+1
- `PlaylistCount() (int, error)`
- `PlaylistPaths() ([]string, error)`
- `Events() <-chan map[string]interface{}`
- `WaitForSocket(socketPath string, timeout time.Duration) error` — polls every 100ms until socket is dialable or timeout elapses

Tests use a mock Unix socket server that speaks the mpv JSON IPC protocol.

**Acceptance criteria:**

- [x] Given no socket file exists, when `Dial()` is called, then it returns an error
- [x] Given a mock mpv socket, when `Command("cycle", "pause")` is called, then the client sends the correct JSON and returns nil on success
- [x] Given a mock mpv socket returning `{"data": 30, "error": "success", "request_id": 1}` for a `get_property volume` request, when `Get("volume")` is called, then it returns `json.RawMessage("30")`
- [x] Given a mock mpv socket, when `InsertNext("/tmp/song.mp3")` is called with an existing playlist of 3 items at position 1, then the correct 4-step sequence is sent (append, get pos, get count, playlist-move)
- [x] Given a mock mpv socket that emits `{"event": "file-loaded"}`, when `Events()` is read, then the map contains `"event": "file-loaded"`
- [x] Given `WaitForSocket` called with a socket that appears after 200ms, when timeout is 1s, then it returns nil
- [x] Given `WaitForSocket` called with a socket that never appears, when timeout is 200ms, then it returns a timeout error
- [x] `go test ./internal/mpv/...` passes

---

### T:4: Implement yt-dlp wrapper

**Description**
Implement `internal/ytdlp/ytdlp.go` and `internal/ytdlp/ytdlp_test.go` as
specified in plan.md section 7.3.

All subprocess calls go through a package-level `var execCommand = exec.Command`
for testability. Tests replace this with a mock.

Functions:
- `Search(binary, query string, n int) ([]Candidate, error)` — runs
  `yt-dlp --dump-json "ytsearch<n>:<query>"`, parses newline-delimited JSON into
  `[]Candidate` (avoid `--flat-playlist` so fields like duration/view_count are present for scoring)
- `Score(c Candidate) int` — scoring algorithm ported exactly from dj-audio
  (all rules in plan.md section 7.3)
- `BestCandidate(binary, query string) (*Candidate, error)` — calls Search with
  n=10, returns the Candidate with the highest Score
- `ResolveURL(binary, url string) (string, error)` — runs
  `yt-dlp --get-url --format bestaudio <url>`, returns direct audio URL
- `Download(binary, url, outDir string) (string, error)` — runs
  `yt-dlp -x --audio-format opus -o "<outDir>/%(id)s.%(ext)s" <url>`,
  returns the path of the downloaded file
- `NormalizeSongKey(human string) string` — lowercase, strip noise words
  ("official", "audio", "video", "ft", "feat", "hd", "4k"), trim whitespace

**Acceptance criteria:**

- [x] Given `Score(Candidate{Title: "Song (Official Audio)", Duration: 200})`, then result is ≥ 40
- [x] Given `Score(Candidate{Title: "Song Live at VMAs", IsLive: true})`, then result is ≤ -100
- [x] Given `Score(Candidate{Duration: 60})`, then result includes the -80 short-clip penalty
- [x] Given `Score(Candidate{Uploader: "ArtistVEVO", ViewCount: 10_000_000, Duration: 210})`, then result includes the VEVO bonus and the 5M view tier bonus
- [x] Given `Score(Candidate{Title: "Song - lyrics nightcore cover", Duration: 200})`, then result includes multiple -30 penalties
- [x] Given a mocked yt-dlp that returns JSON for 3 candidates, when `BestCandidate()` is called, then it returns the candidate with the highest score
- [x] Given `NormalizeSongKey("Britney Spears - Oops! I Did It Again (Official Audio)")`, then result is `"britney spears - oops i did it again"`
- [x] `go test ./internal/ytdlp/...` passes with no yt-dlp installed

---

### T:5: Implement station state management and controller daemon

**Description**
Implement `internal/station/station.go`, `internal/station/controller.go`, and
`internal/station/station_test.go` as specified in plan.md sections 7.4 and 7.5.
Also implement the hidden `claw-radio station daemon` cobra subcommand in
`cmd/station.go` — this is the entry point that `claw-radio start` launches as a
subprocess.

**`station.go`** — manages `station.json` and `state.json` in `cfg.Station.StateDir`:
- `Load(stateDir string) (*Station, error)` — reads both files; missing files return empty state (not error)
- `Save() error` — writes both files atomically (write to temp file, rename)
- `SetSeeds(seeds []string, label string)` — replaces seeds and label, resets round-robin index
- `AppendSeeds(seeds []string)` — adds seeds not already present (normalize before comparing)
- `PickSeed() string` — returns next seed in round-robin order, wrapping around
- `MarkPlayed(videoID, songKey string)` — adds to played_video_ids and played_song_keys
- `AlreadyPlayed(videoID, songKey string) bool` — true if either ID or song key was already played

**`controller.go`** — event-driven queue daemon:
- `Run(cfg *config.Config, prov provider.Provider, log io.Writer)` — main loop
- Connects to mpv IPC with retry/backoff (100ms, 200ms, 400ms, … up to 5s)
- Calls `fillQueue()` immediately on start, then on every `end-file` event
- 30-second safety-net ticker also calls `fillQueue()` in case events are missed
- `fillQueue()` calls `provider.Resolve(seed, cacheDir)` until playlist reaches `queue_depth`
- Skips seeds that `AlreadyPlayed(videoID, songKey)` returns true for
- After filling queue: runs cache GC — deletes files in `cacheDir` not in the
  current mpv playlist, keeping the most recent `queue_depth + 3` files
- Handles SIGTERM: saves state, closes mpv client, returns
- Ensure `stateDir` and `cacheDir` exist before writing files or downloading

**`cmd/station.go`** — hidden cobra subcommand:
- `claw-radio station daemon` — reads config, instantiates `YtDlpProvider`,
  opens log file, calls `controller.Run()`; not shown in `--help`

**Acceptance criteria:**

- [x] Given `SetSeeds(["A", "B", "C"], "label")` then `PickSeed()` three times, then results are `"A"`, `"B"`, `"C"` in order
- [x] Given `PickSeed()` called more than len(seeds) times, then it wraps around to the beginning
- [x] Given `AppendSeeds(["A", "B"])` then `AppendSeeds(["B", "C"])`, then seeds list contains exactly `["A", "B", "C"]` (no duplicate B)
- [x] Given `MarkPlayed("vid123", "song key")`, then `AlreadyPlayed("vid123", "other")` returns true
- [x] Given `MarkPlayed("vid123", "song key")`, then `AlreadyPlayed("vid999", "song key")` returns true (song-key dedupe)
- [x] Given a fresh Load() with no files in stateDir, then both seeds and played lists are empty — no error
- [x] Given a Save() followed by a Load() with the same stateDir, then all fields round-trip correctly
- [x] `go test ./internal/station/...` passes

---

### T:6: Implement provider interface and YtDlpProvider

**Description**
Implement `internal/provider/provider.go`, `internal/provider/ytdlp.go`,
`internal/provider/spotify.go`, `internal/provider/applemusic.go`, and
`internal/provider/provider_test.go` as specified in plan.md section 7.12.

The `Provider` interface has two methods:
```go
Resolve(seed, cacheDir string) (audioPath string, err error)
Name() string
```

`YtDlpProvider` (default, used by the controller):
- Calls `ytdlp.BestCandidate(binary, "ytsearch10:"+seed+" official audio")`
- Checks if `cacheDir/<video_id>.opus` already exists → returns cached path immediately
- Otherwise calls `ytdlp.Download(binary, candidate.WebpageURL, cacheDir)`
- Returns the downloaded file path
- If `seed` is an http/https URL, skip `BestCandidate` and call `ytdlp.Download` directly

`SpotifyProvider` and `AppleMusicProvider` are stubs that return
`fmt.Errorf("<name> provider not implemented")`.

All subprocess calls go through `internal/ytdlp`'s `execCommand` variable —
no separate mock needed here; use the ytdlp package's test helpers.

**Acceptance criteria:**

- [x] Given a mocked yt-dlp that returns a candidate with ID `"abc123"` and the file `cacheDir/abc123.opus` already exists, when `YtDlpProvider.Resolve()` is called, then it returns the cached path without calling Download
- [x] Given a mocked yt-dlp that returns a candidate with ID `"abc123"` and no cached file exists, when `YtDlpProvider.Resolve()` is called, then it calls Download and returns the downloaded path
- [x] Given `seed` is a direct URL, when `YtDlpProvider.Resolve()` is called, then it calls `Download` directly (no `BestCandidate` search)
- [x] Given `SpotifyProvider.Resolve()`, then it returns an error containing "not implemented"
- [x] Given `AppleMusicProvider.Resolve()`, then it returns an error containing "not implemented"
- [x] Given `YtDlpProvider.Name()`, then it returns `"youtube"`
- [x] `go test ./internal/provider/...` passes

---

### T:7: Implement search HTML extractors

**Description**
Implement `internal/search/extract.go` and the extractor tests in
`internal/search/search_test.go` (extractor portion) as specified in plan.md
section 7.11.

All four extractors take an HTML string and return `[]Result` where
`Result{Artist, Title string}`. No external parsing library — use `strings`,
`regexp`, and `bytes` from the standard library.

**`Wikitable(html string) []Result`**:
Find `<table class="wikitable">` blocks. For each table, scan the first `<tr>` for
`<th>` cells. Map column index → "artist" if header text (case-insensitive) contains
"artist", "act", or "performer"; → "title" if it contains "single", "title", "song".
For each subsequent `<tr>`, extract inner text of `<td>` cells at the identified
indices. Strip `[n]` footnote markers (`\[\d+\]`). Skip rows with empty artist or
title.

**`Discogs(html string) []Result`**:
Find all elements with class `tracklist_track_artists` → extract inner text (strip
tags). Find all elements with class `tracklist_track_title` → extract inner text.
If no per-track artists (artist album), get page artist from
`<meta property="og:title" content="Artist – Album">` (split on ` – `). Pair
artists[i] with titles[i] up to min(len).

**`MusicBrainz(html string) []Result`**:
Get release artist from `<meta property="og:title" content="Artist – Release">`.
Find all `<td class="title">` cells for track titles. For Various Artists releases,
check for `<td class="artist">` in the same `<tr>` first; fall back to release
artist. Return one Result per non-empty title.

**`Generic(html string) []Result`**:
Strip all HTML tags. Split into lines. For each line, try three patterns in order:
(a) em-dash: `ARTIST – TITLE` or `ARTIST — TITLE` (U+2013 / U+2014),
(b) hyphen: `ARTIST - TITLE` (both sides 2–80 chars, at least one space each side),
(c) "by": `TITLE by ARTIST` (word boundary, both sides 2–60 chars).
Filter results containing noise keywords: "lyrics", "karaoke", "nightcore",
"instrumental", "cover", "remix", "mix", "1 hour", "extended", "sped up",
"slowed", "reverb", "8d audio", "mashup", "full album", "playlist", "megamix",
"medley", "tribute". Discard results where either field is > 80 or < 2 chars.

**Acceptance criteria:**

- [x] Given a saved Wikipedia "Billboard Year-End Hot 100" HTML fixture with a wikitable, when `Wikitable()` is called, then it returns the correct artist-title pairs for at least 80% of the rows
- [x] Given a saved Discogs compilation tracklist HTML fixture, when `Discogs()` is called, then it returns the correct artist-title pairs for all tracks
- [x] Given a saved MusicBrainz release page HTML fixture, when `MusicBrainz()` is called, then it returns the correct titles with the release artist
- [x] Given `Generic("The Beatles - Hey Jude")`, then it returns `Result{Artist:"The Beatles", Title:"Hey Jude"}`
- [x] Given `Generic("Shape of You by Ed Sheeran")`, then it returns `Result{Artist:"Ed Sheeran", Title:"Shape of You"}`
- [x] Given `Generic("The Beatles – Hey Jude")` (em-dash), then it returns `Result{Artist:"The Beatles", Title:"Hey Jude"}`
- [x] Given `Generic("some song lyrics karaoke nightcore")`, then it returns an empty slice (noise filtered)
- [x] Given `Generic("3:45 - 4:12")`, then it returns an empty slice (timestamp not matched as song)
- [x] `go test ./internal/search/... -run TestExtract` passes

---

### T:8: Implement SearxNG search client

**Description**
Implement `internal/search/search.go` and complete `internal/search/search_test.go`
as specified in plan.md section 7.11.

`NewClient(searxngURL string) *Client` — creates client with 30-second HTTP timeout.

`Search(query string, maxResults int) ([]Result, error)`:
1. Calls `fetchURLs(query, 6)` to get top 6 SearxNG result URLs
2. For each URL, calls `fetchAndExtract(url)` — dispatches to the correct extractor
   based on domain (wikipedia.org → Wikitable, discogs.com → Discogs,
   musicbrainz.org → MusicBrainz, default → Generic)
3. Deduplicates by lowercase `"artist||title"` key
4. Stops fetching once `maxResults` is reached
5. Returns at most `maxResults` results

`fetchURLs(query string, n int) ([]string, error)`:
- GETs `<searxng_url>/search?q=<encoded>&format=json`
- Parses `results[].url` from the JSON response
- Returns up to `n` URLs

If SearxNG is unreachable, return error: `"searxng unreachable at <url>: <cause>"`.
Unreachable individual result pages are silently skipped.

Tests use `httptest.NewServer` to mock both SearxNG and result pages.

**Acceptance criteria:**

- [x] Given a mock SearxNG server returning 3 URLs, when `Search("test", 150)` is called, then `fetchURLs` returns those 3 URLs
- [x] Given mock result pages where Wikipedia returns 50 results and Discogs returns 10, when `Search()` is called, then results are deduplicated and capped at maxResults
- [x] Given the same artist-title pair returned by two different pages, when `Search()` returns, then the pair appears exactly once
- [x] Given SearxNG returns HTTP 500, when `Search()` is called, then it returns an error containing "searxng unreachable"
- [x] Given a result page returns HTTP 404, when `Search()` is called, then it skips that page silently and returns results from the others
- [x] Given a URL containing "wikipedia.org", when `fetchAndExtract()` dispatches, then it calls `Wikitable()`; same for "discogs.com" → `Discogs()`, "musicbrainz.org" → `MusicBrainz()`, other → `Generic()`
- [x] `go test ./internal/search/...` passes

---

### T:9: Implement TTS chain

**Description**
Implement `internal/tts/tts.go` and `internal/tts/tts_test.go` as specified in
plan.md section 7.9.

`Client.Render(text, voicePath, outPath string) error` — three-layer fallback chain:

**Layer 1 — Warm Chatterbox daemon**: Connect to `cfg.TTS.Socket` (Unix socket).
Send JSON: `{"text": text, "out_path": outPath, "voice_prompt": voicePath}` (omit
`voice_prompt` if empty). Read JSON response: `{"status": "ok"}` → return nil;
`{"error": "..."}` → return that error. If connection is refused → fall through.

**Layer 2 — One-shot Chatterbox**: If `cfg.TTS.DataDir/venv/bin/python` exists,
exec `python daemon.py --one-shot <text> <outPath> [--voice <voicePath>]`.
Wait for exit. If exit 0 → return nil. If venv not present → fall through.

**Layer 3 — System TTS**: If `cfg.TTS.FallbackBinary != ""`:
- `say` (macOS): exec `say -o <outPath> <text>`
- `espeak-ng` / `espeak` / `festival`: exec `<binary> -w <outPath> <text>`
If FallbackBinary is empty → return an error with code 4 content:
`"No TTS binary found. Install: claw-radio tts install  OR  apt install espeak-ng"`.

All subprocess calls use a package-level `var execCommand = exec.Command`.

**Acceptance criteria:**

- [x] Given a mock daemon socket that returns `{"status": "ok"}`, when `Render()` is called, then it returns nil and does not fall through to layer 2
- [x] Given a connection refused on the daemon socket and a venv present, when `Render()` is called, then it falls through to layer 2 (one-shot)
- [x] Given connection refused and no venv, when `Render()` is called, then it falls through to layer 3 (system TTS)
- [x] Given connection refused, no venv, and `FallbackBinary == ""`, when `Render()` is called, then it returns an error containing "No TTS binary found"
- [x] Given a non-empty `voicePath` and warm daemon, when `Render()` is called, then the request JSON includes `"voice_prompt": <path>`
- [x] Given an empty `voicePath` and warm daemon, when `Render()` is called, then the request JSON does NOT include a `voice_prompt` field
- [x] `go test ./internal/tts/...` passes

---

## Phase 3: TTS Python daemon

### T:10: Write Chatterbox TTS daemon

**Description**
Write `tts/daemon.py` — the warm Chatterbox TTS daemon as specified in plan.md
section 7.10. This script is embedded in the Go binary and extracted by
`claw-radio tts install`.

**Daemon mode** (default): listens on a Unix socket path passed as argv[1].
For each incoming connection, reads a newline-terminated JSON request:
```json
{"text": "Hello world", "out_path": "/tmp/out.wav", "voice_prompt": "/tmp/voice.wav"}
```
`voice_prompt` is optional. Generates audio using Chatterbox:
```python
wav = model.generate(text, audio_prompt_path=voice_prompt)  # if voice_prompt set
wav = model.generate(text)                                   # if not set
```
Saves to `out_path`. Responds with `{"status": "ok"}` or `{"error": "..."}`.
Keeps the model in memory across requests (do not reload per request).

**GPU detection** (called once at startup):
```python
def get_device():
    if torch.backends.mps.is_available(): return "mps"
    if torch.cuda.is_available():         return "cuda"
    return "cpu"
```

**One-shot mode**: `python daemon.py --one-shot <text> <out_path> [--voice <wav>]`
Loads model, generates once, saves, exits. For use when the daemon is not warm.

**Graceful shutdown**: handles SIGTERM by closing the socket and exiting cleanly.

**Acceptance criteria:**

- [x] Given daemon mode and a request without `voice_prompt`, when the request is processed, then `model.generate(text)` is called and the output file is written
- [x] Given daemon mode and a request with `voice_prompt`, when the request is processed, then `model.generate(text, audio_prompt_path=...)` is called
- [x] Given an invalid `out_path`, when the request is processed, then the response contains `{"error": "..."}` and the daemon continues accepting new connections
- [x] Given `--one-shot "Hello" /tmp/out.wav`, when run, then the file `/tmp/out.wav` is created and the process exits 0
- [x] Given `--one-shot "Hello" /tmp/out.wav --voice /tmp/voice.wav`, when run, then voice cloning is used
- [x] Given SIGTERM in daemon mode, when the signal arrives, then the process exits cleanly without traceback

---

### T:11: Create embedded TTS assets and go:embed setup

**Description**
Create the `tts/voices/` directory with default reference WAV files and a README,
and wire up `go:embed` so the binary bundles the entire `tts/` directory.

Create three minimal valid WAV files (short samples are fine) as default references:
- `tts/voices/pop.wav`
- `tts/voices/country.wav`
- `tts/voices/electronic.wav`

Create `tts/voices/README.md` explaining that these are default voice reference
files, and that the agent can replace them by running `claw-radio tts voice add <url>`.

In `cmd/tts.go` (or a dedicated `embed.go`), declare:
```go
//go:embed tts
var ttsFS embed.FS
```

Verify that `tts/daemon.py` content is readable from the embedded FS at build time.

**Acceptance criteria:**

- [x] Given `go build ./...`, then it succeeds — the embed compiles without errors
- [x] Given a test that calls `ttsFS.ReadFile("tts/daemon.py")`, then it returns non-empty content
- [x] Given a test that calls `ttsFS.ReadFile("tts/voices/pop.wav")`, then it returns a valid WAV header (first 4 bytes = `RIFF`)
- [x] `tts/voices/README.md` exists and explains the default nature of the files and how to replace them

---

## Phase 4: CLI commands

### T:12: Implement main.go, root command, and exit code handling

**Description**
Implement `main.go` and `cmd/root.go` as specified in plan.md sections 7.14 and 6.

Define the `exitError` struct:
```go
type exitError struct{ err error; code int }
func (e *exitError) Error() string { return e.err.Error() }
func exitCode(err error, code int) *exitError { return &exitError{err, code} }
```

In `main()`: call `cmd.Execute()`. If it returns an `*exitError`, call
`os.Exit(code)`. If it returns any other non-nil error, call `os.Exit(1)`.

Root command: binary name `claw-radio`, short description. Register all
subcommands. Inject version at build time via `-ldflags "-X main.version=<v>"`.
`claw-radio version` prints the version string.

**Acceptance criteria:**

- [x] Given no arguments, when `claw-radio` is run, then it prints help text and exits 0
- [x] Given `claw-radio version`, then it prints a version string (e.g. `claw-radio dev` when no ldflags set) and exits 0
- [x] Given a command returns `exitCode(err, 5)`, then the process exits with code 5
- [x] Given an unknown subcommand, when `claw-radio foobar` is run, then it exits 2 (cobra default for bad usage)

---

### T:13: Implement start and stop commands

**Description**
Implement `cmd/start.go` and `cmd/stop.go` as specified in plan.md section 7.12
(the `cmd/start.go` subsection).

**`claw-radio start`**:
1. Resolve mpv binary via `cfg.MPV.Binary` (already resolved by config loader).
   If empty (not found), exit 4 with message:
   ```
   mpv not found.
   Install on macOS:  brew install mpv
   Install on Linux:  apt install mpv   (Debian/Ubuntu)
                      dnf install mpv   (Fedora)
   ```
   Same check for yt-dlp.
2. Best-effort: if socket file exists, connect and send `quit`; ignore errors.
3. If any `/tmp/claw-radio-*.pid` files exist, send SIGTERM to those PIDs (ignore "no such process") and remove stale PID files before continuing.
4. Launch mpv as detached background process with flags:
   `--no-video --idle=yes --force-window=no --audio-display=no --cache=yes
    --cache-secs=20 --demuxer-max-bytes=50MiB --input-ipc-server=<socket>`
   Redirect stdout/stderr to `cfg.MPV.Log`.
   Ensure the parent directory of `cfg.MPV.Log` exists.
5. Write mpv PID to `/tmp/claw-radio-mpv.pid`.
6. Call `mpv.WaitForSocket(cfg.MPV.Socket, 5*time.Second)` — exit 1 on timeout.
7. Launch `exec.Command(os.Executable(), "station", "daemon")`, redirect to
   log file, call `.Start()`. Write PID to `/tmp/claw-radio-controller.pid`.
8. If `cfg.TTS.DataDir/venv/bin/python` exists, launch daemon.py as background
   process with socket path. Write PID to `/tmp/claw-radio-tts.pid`.
9. Print: `claw-radio started`.

**`claw-radio stop`**:
1. Read all `/tmp/claw-radio-*.pid` files. For each: parse PID, send SIGTERM
   (ignore "no such process" errors).
2. If mpv socket exists: best-effort connect and send `{"command":["quit"]}`.
3. Remove all `/tmp/claw-radio-*.pid` files and socket files.
4. Print: `claw-radio stopped`.

**Acceptance criteria:**

- [x] Given mpv is not on PATH, when `claw-radio start` is run, then it exits 4 and the error message contains "brew install mpv" and "apt install mpv"
- [x] Given yt-dlp is not on PATH, when `claw-radio start` is run, then it exits 4 with yt-dlp install instructions
- [x] Given stale PID files exist, when `claw-radio start` runs, then it still starts mpv/controller and replaces the PID files
- [x] Given start completes successfully, then `/tmp/claw-radio-mpv.pid` and `/tmp/claw-radio-controller.pid` exist with valid PIDs
- [x] Given start completes successfully, when `claw-radio stop` is run, then the PID files are removed and processes are terminated
- [x] Given no PID files exist, when `claw-radio stop` is run, then it exits 0 without error
- [x] Given mpv IPC socket doesn't appear within 5 seconds, when `claw-radio start` is run, then it exits 1 with a timeout error

---

### T:14: Implement playback control commands

**Description**
Implement `cmd/play.go` with five subcommands under `claw-radio`, as specified in
plan.md sections 6 and 7.6.

All commands require mpv to be running — check by attempting to dial the socket;
if connection refused, exit 5 with: `"mpv not running. Start with: claw-radio start"`.

**`claw-radio play <query|url>`**: resolves the argument to a local audio file via
`provider.YtDlpProvider.Resolve()` (query or direct URL), then calls `InsertNext(path)`
followed by `Command("playlist-next")`. This puts the song immediately after the
current track and skips to it.

**`claw-radio queue <query|url>`**: resolves argument (query or direct URL), calls `LoadFile(path, "append")`.

**`claw-radio pause`**: calls `Set("pause", true)`.

**`claw-radio resume`**: calls `Set("pause", false)`.

**`claw-radio next`**: calls `Command("playlist-next")`.

**Acceptance criteria:**

- [x] Given mpv is not running, when any playback command is run, then it exits 5 with a message suggesting `claw-radio start`
- [x] Given a valid query `"Daft Punk - Get Lucky"`, when `claw-radio play` is called, then `InsertNext` and `playlist-next` are called in sequence (verify via mock mpv socket)
- [x] Given a direct URL, when `claw-radio queue` is called, then `YtDlpProvider.Resolve()` is invoked with the URL and the resolved file is appended
- [x] Given a valid query, when `claw-radio queue` is called, then `LoadFile(path, "append")` is called
- [x] Given mpv is running, when `claw-radio pause` is called, then `set_property pause true` is sent
- [x] Given mpv is running, when `claw-radio resume` is called, then `set_property pause false` is sent
- [x] Given mpv is running, when `claw-radio next` is called, then `playlist-next` command is sent

---

### T:15: Implement seed command

**Description**
Implement `cmd/seed.go` with one command: `claw-radio seed '<json-array>'
[--label "<vibe>"] [--append]` as specified in plan.md section 6.

Parse the first positional argument as a JSON array of strings. If the JSON is
invalid or not an array of strings, exit 1 with a clear parse error.

Without `--append`: calls `station.SetSeeds(seeds, label)` — replaces the existing
seed list and label. Prints: `"Seeded N songs (label: <label>)"` or
`"Seeded N songs"` if no label.

With `--append`: calls `station.AppendSeeds(seeds)` — adds to the existing list
without overwriting. Ignores `--label` flag when appending. Prints:
`"Added M songs (total: N)"`.

Saves station state after modification.

**Acceptance criteria:**

- [x] Given a valid JSON array `'["A - B", "C - D"]'`, when `seed` is called, then `station.json` contains those two seeds
- [x] Given `--label "2000s pop"`, when `seed` is called, then `station.json` has `"label": "2000s pop"`
- [x] Given an existing seed list of 5 and `seed '["X - Y"]' --append`, when run, then total seeds is 6
- [x] Given `--append` and a seed already in the list, when run, then the seed appears only once (deduplication)
- [x] Given invalid JSON `'not-json'`, when `seed` is called, then it exits 1 with a parse error message
- [x] Given a JSON number instead of array `'42'`, when `seed` is called, then it exits 1
- [x] Given no argument, when `claw-radio seed` is called, then it exits 2 with usage help

---

### T:16: Implement search command

**Description**
Implement `cmd/search.go` with one command: `claw-radio search "<query>"` as
specified in plan.md section 7.11 (`cmd/search.go` subsection).

Loads config, creates `search.NewClient(cfg.Search.SearxNGURL)`, calls
`Search(query, 150)`. Formats results as `"Artist - Title"` strings.

stdout: JSON array (newline-terminated): `["Artist - Title", ...]`
stderr: `"Fetched <pages> pages, extracted N unique songs."`

If SearxNG is unreachable, exit 1 with the error message on stderr.
If query argument is missing, exit 2.

**Acceptance criteria:**

- [x] Given a running mock SearxNG and `claw-radio search "test query"`, when run, then stdout is a valid JSON array of strings and stderr contains pages fetched and the count
- [x] Given stdout output, when parsed as JSON, then each element is in `"Artist - Title"` format
- [x] Given SearxNG unreachable (port closed), when `claw-radio search` is called, then it exits 1 and stderr contains "searxng unreachable"
- [x] Given no query argument, when `claw-radio search` is called, then it exits 2
- [x] Output: `["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]` on stdout; `"Fetched 1 pages, extracted 2 unique songs."` on stderr

---

### T:17: Implement say command

**Description**
Implement `cmd/say.go` with one command: `claw-radio say "<text>" [--voice <name|path>]`
as specified in plan.md section 7.7.

Voice resolution order (via `resolveVoicePath`):
1. `cfg.TTS.Voices[name]` — explicit config map entry (if non-empty)
2. `cfg.TTS.DataDir/voices/<name>.wav` — auto-discovered after `tts voice add`
3. Literal file path (if `name` passes `os.Stat`)
4. Empty string → Chatterbox uses its built-in default voice

Steps:
1. Resolve voice WAV path via `resolveVoicePath(voiceFlag, cfg)`
2. Create `cfg.TTS.DataDir/banter/` directory if not exists
3. Generate output path: `cfg.TTS.DataDir/banter/<unix_timestamp_ns>.wav`
4. Call `ttsClient.Render(text, voicePath, outPath)`
5. Call `mpvClient.InsertNext(outPath)` to queue the banter after current track
6. Print: `"queued banter"`

Requires mpv running → exit 5.
If TTS unavailable → exit 4.

**Acceptance criteria:**

- [ ] Given mpv not running, when `claw-radio say "hello"` is called, then it exits 5
- [ ] Given TTS unavailable and mpv running, when `claw-radio say "hello"` is called, then it exits 4
- [ ] Given `--voice pop` and `data_dir/voices/pop.wav` exists, when `say` is called, then `tts.Render` receives `data_dir/voices/pop.wav` as the voice path
- [ ] Given `--voice /absolute/path/voice.wav` and that file exists, when `say` is called, then `tts.Render` receives that literal path
- [ ] Given `--voice unknown` and no file by that name, when `say` is called, then `tts.Render` receives empty string (Chatterbox default voice)
- [ ] Given successful render and InsertNext, when `say` completes, then it prints "queued banter" and exits 0
- [ ] Given no text argument, when `claw-radio say` is called, then it exits 2

---

### T:18: Implement tts commands

**Description**
Implement `cmd/tts.go` with two subcommands under `claw-radio tts`, as specified
in plan.md section 7.7.

**`claw-radio tts install`**:
1. Check python3 is available via `exec.LookPath("python3")` — exit 4 if not found
2. Create `cfg.TTS.DataDir/venv/` via `python3 -m venv <path>`
3. Extract `tts/daemon.py` from the embedded FS to `cfg.TTS.DataDir/daemon.py`
4. Extract all files from `tts/voices/` to `cfg.TTS.DataDir/voices/`
5. Run `<venv>/bin/pip install chatterbox-tts torch torchaudio`
6. Detect if CUDA available by checking `torch.cuda.is_available()` in the venv;
   if not, print a warning: "CUDA not available — using CPU (slow on Linux VPS)"
7. Print: `"Chatterbox TTS installed. Use: claw-radio say \"<text>\""`

**`claw-radio tts voice add <url> [--name <name>]`**:
1. Requires yt-dlp and ffmpeg — exit 4 if either not found (use `cfg.YtDlp.Binary` / `cfg.FFmpeg.Binary`)
2. Run: `<yt-dlp> -x --audio-format wav -o "<data_dir>/voices/<name>.%(ext)s" <url>`
3. If `--name` not provided, use the sanitized video title from yt-dlp's output
4. If resulting WAV is longer than 30 seconds, trim to 30s using:
   `<ffmpeg> -i <in> -t 30 -y <out>`
5. Print: `"Voice '<name>' saved → <path>"`

**Acceptance criteria:**

- [ ] Given python3 not on PATH, when `tts install` is run, then it exits 4 with a python3 install message
- [ ] Given python3 available (mocked), when `tts install` completes, then `data_dir/daemon.py` exists with the correct content from the embedded FS
- [ ] Given `tts install` completes, then `data_dir/voices/pop.wav` exists (extracted from embed)
- [ ] Given yt-dlp not on PATH, when `tts voice add` is run, then it exits 4
- [ ] Given ffmpeg not on PATH, when `tts voice add` is run, then it exits 4 with ffmpeg install instructions
- [ ] Given a mocked yt-dlp that downloads a WAV and mocked ffmpeg, when `tts voice add <url> --name country` is called, then `data_dir/voices/country.wav` exists and prints the confirmation message
- [ ] Given no URL argument, when `tts voice add` is called, then it exits 2

---

### T:19: Implement events command

**Description**
Implement `cmd/events.go` with one command: `claw-radio events [--json]` as
specified in plan.md sections 6 and 7.8.

Connects to the mpv IPC socket (exit 5 if not running). Reads from `client.Events()`
in a loop and translates mpv events to the claw-radio event schema:

- mpv `file-loaded` → query `media-title` and `duration` via `client.Get()`, emit
  `{"event":"track_started","title":"...","duration":211.0,"ts":<unix>}`
- mpv `end-file` → emit `{"event":"track_ended","title":"<lastTitle>","ts":<unix>}`;
  then check `PlaylistCount()` — if ≤ 2, also emit
  `{"event":"queue_low","count":<n>,"depth":<cfg.Station.QueueDepth>,"ts":<unix>}`
- When the events channel closes (mpv disconnected), emit
  `{"event":"engine_stopped","ts":<unix>}` and exit 0

With `--json`: emit newline-delimited JSON, flush after each line.
Without `--json`: emit human-readable text, e.g.
`"▶ track_started: Britney Spears - Oops! (3:31)"`.

**Acceptance criteria:**

- [ ] Given mpv not running, when `claw-radio events` is called, then it exits 5
- [ ] Given a mock mpv socket emitting `file-loaded`, when `events --json` is running, then stdout contains `{"event":"track_started",...}` with title and duration fields
- [ ] Given a mock mpv socket emitting `end-file` with 1 item remaining in playlist, when `events --json` is running, then stdout contains both `track_ended` and `queue_low` events
- [ ] Given the mock mpv socket closes, when `events --json` is running, then `engine_stopped` is emitted and the process exits 0
- [ ] Given `events` without `--json`, then output is human-readable (no JSON braces on the track_started line)
- [ ] Each JSON event line is flushed immediately (not buffered)

---

### T:20: Implement status command

**Description**
Implement `cmd/status.go` with one command: `claw-radio status [--json]` as
specified in plan.md section 7.13.

Collects the following, best-effort (missing components degrade gracefully):
- **engine**: `"running"` if mpv PID file exists and process alive, else `"stopped"`
- **station**: reads `station.json` — `{label, seeds: <count>}`; omit if no file
- **playback**: if mpv running, query `pause`, `media-title`, `time-pos`,
  `duration`, `volume` via IPC — `{state, title, time_pos, duration, volume}`
- **queue**: `{count: PlaylistCount(), depth: cfg.Station.QueueDepth}`
- **controller**: `"running"` if controller PID file exists and process alive, else `"stopped"`
- **tts**: `"warm"` if TTS socket responds, `"system"` if FallbackBinary set, `"unavailable"` otherwise

With `--json`: output the schema from plan.md section 7.13.
Without `--json`: print the same info in human-readable format.

**Acceptance criteria:**

- [ ] Given mpv not running, when `status --json` is called, then `"engine": "stopped"` in output and it exits 0 (not an error)
- [ ] Given mpv running and a mock IPC, when `status --json` is called, then output is valid JSON containing `"engine": "running"` and `"playback"` with title/volume/state fields
- [ ] Given `station.json` exists with 48 seeds, when `status --json` is called, then output contains `"station": {"seeds": 48}`
- [ ] Given TTS daemon socket responding, when `status --json` is called, then `"tts": "warm"`
- [ ] Given no TTS socket but FallbackBinary set, when `status --json` is called, then `"tts": "system"`
- [ ] Output matches schema: `{"engine":"running","station":{"label":"...","seeds":48},"playback":{...},"queue":{...},"controller":"running","tts":"warm"}`

---

## Phase 5: Infrastructure

### T:21: Create config.example.json

**Description**
Create `config.example.json` in the repo root as a reference template. All fields
should be present and set to their defaults (binary fields as empty string `""`).
Matches the schema from plan.md section 5 exactly.

**Acceptance criteria:**

- [ ] Given `config.example.json` loaded via `CLAW_RADIO_CONFIG=config.example.json`, when `config.Load()` is called, then it parses without error
- [ ] All top-level keys are present: `mpv`, `ytdlp`, `ffmpeg`, `tts`, `station`, `search`
- [ ] All binary fields are set to `""` (empty string, meaning auto-detect)
- [ ] `station.queue_depth` is `5`, `search.searxng_url` is `"http://localhost:8888"`

---

### T:22: Add CI workflow

**Description**
Create `.github/workflows/ci.yml` as specified in plan.md section 11.

Triggers on `push` and `pull_request` to `main`. Runs on `ubuntu-latest`.
Steps:
1. `actions/checkout@v4`
2. `actions/setup-go@v5` with `go-version-file: go.mod`
3. Format check: `if [ -n "$(gofmt -l .)" ]; then gofmt -l .; exit 1; fi`
4. Vet: `go vet ./...`
5. Test: `go test ./...`

**Acceptance criteria:**

- [ ] Given correctly formatted code pushed to main, then the CI workflow passes all steps
- [ ] Given a file with incorrect gofmt formatting committed, then the format check step fails
- [ ] Given `go test ./...` runs in CI, then it passes with zero external tools installed (all tests mock their dependencies)

---

### T:23: Add GoReleaser config and release workflow

**Description**
Create `.goreleaser.yaml` and `.github/workflows/release.yml` as specified in
plan.md section 9.

`.goreleaser.yaml`:
- Version 2
- `CGO_ENABLED=0`, targets: `darwin_amd64`, `darwin_arm64`, `linux_amd64`, `linux_arm64`
- `universal_binaries` merging the two darwin targets, `replace: true`
- ldflags: `-s -w -X main.version={{.Version}}`
- Archive includes `config.example.json` and `README.md`
- `homebrew_casks` entry for `vossenwout/homebrew-tap` with quarantine removal hook

Release workflow triggers on `v*` tag push, runs on `ubuntu-latest`, uses
`goreleaser/goreleaser-action@v6`.

Also: create the `vossenwout/homebrew-tap` GitHub repo if it does not exist, and
add `HOMEBREW_TAP_GITHUB_TOKEN` as a repository secret on the claw-radio repo.

**Acceptance criteria:**

- [ ] Given the `.goreleaser.yaml`, when `goreleaser check` is run locally, then it reports no errors
- [ ] The `homebrew_casks` section references `vossenwout/homebrew-tap` and binary `claw-radio`
- [ ] Given a `v*` tag is pushed, then the release workflow triggers and a GitHub Release is created
- [ ] Given the release completes, then `Casks/claw-radio-cli.rb` is created or updated in `vossenwout/homebrew-tap`

---

## Phase 6: Documentation

### T:24: Write SKILL.md

**Description**
Write `SKILL.md` using the full design from plan.md section 13. This file is
read by the AI agent to understand how to operate the radio. It must cover:
- YAML frontmatter (`name`, `description`)
- Persona section (voice styles per genre)
- Building a seed list (all three vibe types with query examples)
- Full startup flow (with event loop pseudocode)
- Reacting to events (`track_started`, `queue_low`, `track_ended`, `engine_stopped`)
- Stopping

**Acceptance criteria:**

- [ ] The file has valid YAML frontmatter with `name: claw-radio` and a `description` that covers: start radio, build seed list, inject banter, react to events
- [ ] The persona section lists at least 6 genre voice styles
- [ ] The seed-building section includes query examples for era/genre, artist-based, and mood/abstract vibes
- [ ] The startup flow shows the full command sequence ending with the events loop
- [ ] `track_started` handler specifies banter before **every** song

---

### T:25: Write README.md

**Description**
Write `README.md` for human readers covering: what claw-radio is, installation
(Homebrew + Linux curl), required dependencies with install commands (mpv, yt-dlp,
ffmpeg), optional TTS setup (`claw-radio tts install` and `tts voice add`), and
a complete CLI reference for all commands and flags.

**Acceptance criteria:**

- [ ] Installation section includes `brew install vossenwout/tap/claw-radio-cli` for macOS
- [ ] Installation section includes the `curl` + `tar` command for Linux
- [ ] Dependency section includes install commands for mpv, yt-dlp, and ffmpeg on both platforms
- [ ] CLI reference lists all commands: `tts install`, `tts voice add`, `start`, `stop`, `play`, `queue`, `pause`, `resume`, `next`, `seed`, `search`, `say`, `events`, `status`, `version`
- [ ] Each CLI entry includes its flags and a one-line description
