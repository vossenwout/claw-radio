# Implementation Plan: claw-radio

---

## 1. Overview

`claw-radio` is a GTA-style radio station CLI designed to be **operated by an AI agent**. Think of it as the radio studio control board — the machinery. The AI agent (OpenClaw, which runs on Claude) is the radio host who operates the board: it knows what songs fit a vibe, decides when to speak, and generates banter text. The CLI renders audio and reacts to commands.

**Design principle: agent-first.** The tool never decides anything. It accepts commands, executes them faithfully, and streams events back so the agent knows what's happening.

**Scope:**

- Start/stop the playback engine (mpv) and auto-queue controller
- Web search via SearxNG: given a raw query string, fetch pages, parse (artist, title) pairs, return JSON — the agent calls this multiple times to build a seed list
- Accept a curated seed list from the agent and auto-queue songs sourced from YouTube (yt-dlp)
- Accept banter text from the agent and render it as speech via TTS, inserted between songs
- Stream playback events (track started, ended, queue low) as JSON for the agent to consume
- Full playback control: play (front of queue), queue (append), pause, resume, next, stop

**Out of scope (handled by the agent):**

- Search query generation — the agent decides which queries to run based on the vibe, calls `search` for each, accumulates and curates results, then seeds
- Banter text generation — agent composes banter, passes text to `claw-radio say`
- Telegram/notifications — agent reads `claw-radio events` and sends them however it wants
- Volume control — agent uses system audio tools or accepts the default

**Platform:** Linux and macOS. Cross-platform Go binary. The only platform-specific concern is the optional TTS daemon's GPU acceleration (MPS on Apple Silicon, CUDA on Linux with NVIDIA GPU, CPU fallback on both).

---

## 2. Language

**Go CLI binary + optional Python TTS subprocess.**

### Go is cross-platform — with one nuance

Go compiles natively to any target OS and architecture (`GOOS=linux GOARCH=amd64`,
`GOOS=darwin GOARCH=arm64`, etc.). The standard library abstracts file paths,
signals, sockets, and process management across platforms.

Background process management (spawning mpv and the controller as daemons) uses
standard `os/exec` + PID files — no OS-specific service managers needed.

### Why Go for the CLI

- Single static binary — no runtime, no interpreter, no venv, no version management.
- `net` package speaks directly to mpv's Unix IPC socket — no `nc` subprocess.
- `os/exec` wraps `yt-dlp` and `ffmpeg`.
- `exec.LookPath` finds binaries wherever they are installed (Homebrew, apt, etc.) without hardcoded paths.
- GoReleaser produces binaries for all targets from a single CI run.

### Why Python for TTS (optional)

Chatterbox Turbo is a Python ML library. There is no Go port. The daemon pattern
(model stays in memory, receives requests via Unix socket) is the right approach
regardless of language.

The daemon script (`daemon.py`) is embedded in the Go binary at compile time using
`go:embed` — a standard Go library feature that bundles files into the binary so
users download a single self-contained executable. When the user runs
`claw-radio tts install`, the binary extracts `daemon.py` from itself and creates
a Python venv. This is a common pattern in Go tools that ship helper scripts.

**Chatterbox is optional.** Without `claw-radio tts install`, the `say` command
falls back to system TTS (`say` on macOS, `espeak-ng` on Linux) — no setup,
no dependencies. The fallback is robotic but functional.

**No CGO.** `CGO_ENABLED=0` at build time.

---

## 3. Dependencies

### Go module dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework — subcommands, flags, help |

No other third-party Go packages. All functionality uses the standard library
(`net`, `os/exec`, `encoding/json`, `net/http`).

### External tool dependencies

| Tool | macOS install | Linux install | Purpose |
|---|---|---|---|
| `mpv` | `brew install mpv` | `apt install mpv` | Playback engine with JSON IPC |
| `yt-dlp` | `brew install yt-dlp` | `pip install yt-dlp` | YouTube audio resolution + download |
| `ffmpeg` | `brew install ffmpeg` | `apt install ffmpeg` | Audio post-processing |
| `python3` *(optional)* | Xcode CLT / brew | `apt install python3 python3-venv` | Chatterbox TTS daemon runtime |
| `espeak-ng` *(optional, Linux only)* | ships with macOS (`say`) | `apt install espeak-ng` | System TTS fallback when Chatterbox is not installed |
| SearxNG *(optional)* | `docker run searxng/searxng` | `docker run searxng/searxng` | Self-hosted meta-search engine for `claw-radio search` |

All binaries are located via `exec.LookPath` at runtime. Absolute paths can be
overridden in config for non-standard installs.

### Python TTS dependencies — optional, managed by `claw-radio tts install`

Creates `<data_dir>/venv/` and installs:

```
chatterbox-tts
torch
torchaudio
```

On Linux with CUDA, `torch` with CUDA support should be installed manually first.
`claw-radio tts install` detects this and prints a warning if CUDA is unavailable.
CPU fallback is automatic.

---

## 4. Repository structure

```
claw-radio/
├── SKILL.md
├── README.md
├── research.md                        (already exists)
├── plan.md                            (this file)
├── config.example.json
├── go.mod                             module: github.com/vossenwout/claw-radio
├── go.sum
├── main.go
├── cmd/
│   ├── root.go                        cobra root, exit code handling
│   ├── tts.go                         tts install — extract daemon, create venv
│   ├── start.go                       start mpv engine + controller daemon
│   ├── stop.go                        stop everything cleanly
│   ├── play.go                        play / queue / pause / resume / next / stop
│   ├── seed.go                        set song list for auto-queue controller
│   ├── search.go                      single-query web search → (artist, title) JSON
│   ├── say.go                         render agent-provided banter text as TTS
│   ├── events.go                      stream JSON events to stdout for agent
│   └── status.go                      status [--json]
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── mpv/
│   │   ├── client.go                  Unix socket IPC client
│   │   └── client_test.go
│   ├── ytdlp/
│   │   ├── ytdlp.go                   search, score, resolve, download
│   │   └── ytdlp_test.go
│   ├── station/
│   │   ├── station.go                 seed list + state (load/save)
│   │   ├── controller.go              event-driven queue management daemon
│   │   └── station_test.go
│   ├── tts/
│   │   ├── tts.go                     TTS chain: chatterbox daemon → one-shot → system TTS
│   │   └── tts_test.go
│   ├── search/
│   │   ├── search.go                  SearxNG HTTP client + orchestration
│   │   ├── extract.go                 HTML parsers: Wikipedia, Discogs, MusicBrainz, generic
│   │   └── search_test.go
│   └── provider/
│       ├── provider.go                Provider interface
│       ├── ytdlp.go                   YtDlpProvider — default, searches YouTube
│       ├── spotify.go                 SpotifyProvider (future — stub only)
│       ├── applemusic.go              AppleMusicProvider (future — stub only)
│       └── provider_test.go
├── tts/
│   ├── daemon.py                      Chatterbox Turbo daemon (go:embedded into binary)
│   └── voices/                        Reference WAV files for per-genre voice cloning
│       ├── README.md
│       ├── pop.wav
│       ├── country.wav
│       └── electronic.wav
├── .goreleaser.yaml
└── .github/
    └── workflows/
        ├── ci.yml
        └── release.yml
```

The `tts/` directory is embedded into the Go binary at build time:

```go
//go:embed tts
var ttsFS embed.FS
```

`claw-radio tts install` extracts `tts/daemon.py` and `tts/voices/` to the data
directory and creates the Python venv there.

---

## 5. Config schema

Location: `~/.config/claw-radio/config.json`
Override: `CLAW_RADIO_CONFIG=/path/to/config.json`

**The config file is entirely optional.** The tool works out of the box with zero
configuration using sensible defaults. The config file only exists as an escape
hatch for non-standard setups — e.g., mpv installed in an unusual path, wanting
the IPC socket at a different location, or adjusting the queue depth.

Most users (and agents running on a standard install) will never need to create
this file.

```json
{
  "mpv": {
    "socket": "/tmp/claw-radio-mpv.sock",
    "binary": "",
    "log":    "~/.local/share/claw-radio/mpv.log"
  },
  "ytdlp": {
    "binary": ""
  },
  "ffmpeg": {
    "binary": ""
  },
  "tts": {
    "socket":          "/tmp/claw-radio-tts.sock",
    "data_dir":        "~/.local/share/claw-radio",
    "fallback_binary": "",
    "voices": {
      "pop":        "",
      "country":    "",
      "electronic": "",
      "default":    ""
    }
  },
  "station": {
    "queue_depth": 5,
    "cache_dir":   "~/.local/share/claw-radio/cache",
    "state_dir":   "~/.local/share/claw-radio/state"
  },
  "search": {
    "searxng_url": "http://localhost:8888"
  }
}
```

### What each field does (only change if you have a non-standard setup)

**`mpv.binary` / `ytdlp.binary` / `ffmpeg.binary`** — empty string = auto-detect
via `exec.LookPath`. Only set these if the binary is installed somewhere not on
your `$PATH` (rare).

**`mpv.socket`** — the Unix socket file mpv listens on. Both mpv and claw-radio
use this path to communicate. Only change if you need multiple mpv instances.

**`tts.fallback_binary`** — system TTS binary used when Chatterbox isn't installed.
Auto-detected: `say` on macOS, `espeak-ng` on Linux. Only override if you prefer
a different TTS tool.

**`tts.voices`** — map of genre → reference WAV file for Chatterbox voice cloning.
Defaults to the bundled voice files after `claw-radio tts install`.

**`station.queue_depth`** — how many songs to download and queue in advance. Default
5. Higher = smoother playback on slow connections, more disk usage.

**`search.searxng_url`** — URL of your SearxNG instance. Defaults to
`http://localhost:8888`, which is where OpenClaw's SearxNG service runs. For
standalone users: run SearxNG with Docker and configure this URL. If SearxNG
is unreachable, `claw-radio search` exits with a clear error.

---

## 6. CLI design

```
claw-radio tts install                              # optional: install Chatterbox for high-quality voice
claw-radio tts voice add <url> [--name <name>]      # download a YouTube clip as a voice reference WAV

claw-radio start                               # start mpv engine + controller in background
claw-radio stop                                # stop everything cleanly

claw-radio play  <query|url>                   # insert at front of queue, skip to it immediately
claw-radio queue <query|url>                   # append to end of queue
claw-radio pause
claw-radio resume
claw-radio next

claw-radio search "<query>"                             # single web search → JSON array of "Artist - Title" strings
claw-radio seed  '<json-array>' [--label "<vibe>"]      # set song list for auto-queue controller
claw-radio say   "<text>" [--voice <name|/path/to.wav>] # render agent banter as TTS, insert after current

claw-radio events [--json]                     # stream JSON events to stdout
claw-radio status [--json]                     # current state snapshot

claw-radio version
```

### What each command does

**`tts install`** — optional, run once. Installs Chatterbox TTS daemon into a Python
venv in `~/.local/share/claw-radio/`. Without this, `say` falls back to system TTS
(`say` on macOS, `espeak-ng` on Linux) which sounds robotic but requires no setup
beyond installing the binary (see Dependencies).
<comment>This isn't a known limitation its just an external dependency that should be in the external dependency section for linux</comment>

**`tts voice add <url> [--name <name>]`** — downloads a YouTube video as a voice
reference WAV (via yt-dlp + ffmpeg) and saves it to `<data_dir>/voices/<name>.wav`.
The agent uses this to match the station voice to the genre — e.g., it searches
YouTube for a hillbilly radio DJ clip and downloads it as the `country` voice profile.
If `--name` is omitted, the sanitized video title is used. Requires Chatterbox
(`tts install`) to actually use the voice; voice download itself has no ML dependency.

**`start`** — starts mpv (playback engine) and the station controller (auto-queue daemon)
as background processes. PIDs written to `/tmp/claw-radio-*.pid`.
Chatterbox daemon started automatically if the venv exists.

**`stop`** — kills mpv, controller, and TTS daemon by PID. Cleans up socket files.

**`play <query>`** — inserts the song at position 1 in the queue (right after current
track) and skips to it immediately. The rest of the queue is preserved. The controller
then refills the queue behind it.

**`queue <query>`** — appends to end of queue, plays when reached naturally.

**`search "<query>"`** — submits a single raw query string to SearxNG, fetches the
top result pages, and runs site-specific HTML parsers to extract (artist, title)
pairs. Returns up to 150 results as a JSON array of `"Artist - Title"` strings on
stdout. A human-readable summary (pages fetched, songs found) goes to stderr so
stdout stays clean for piping. Does not auto-seed — the agent reviews, curates, and
calls `seed` when satisfied.

```bash
claw-radio search "Billboard Year-End Hot 100 2001 site:wikipedia.org"
# stdout: ["Lifehouse - Hanging By A Moment", "Lil' Romeo - My Baby", ...]
# stderr: Fetched 4 pages, extracted 87 songs.
```

The agent calls this multiple times with different queries, accumulates results
across calls, deduplicates with its own intelligence, supplements with songs it
knows from its training, and calls `seed` when the list is good enough.

**`seed '<json-array>' [--label "..."]`** — provides the agent's curated song list
to the controller. The controller uses this to auto-queue and refill. `--label` is
optional metadata for `status` display. Replaces any existing seed list by default;
`--append` adds to the existing list without overwriting.

```bash
claw-radio seed '["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]' \
    --label "2000s bubblegum pop"
```

**`say "<text>" [--voice <genre>]`** — the agent composes banter text and passes it
here. The tool renders it as speech (via Chatterbox or system TTS), then inserts the
audio file right after the current track in the mpv queue. Returns exit 0 when the
file is queued; playback happens when the current song ends.

**`events [--json]`** — long-running command that connects to mpv IPC and streams
events as they happen. Without `--json`, output is human-readable (interactive
debugging). With `--json`, newline-delimited JSON — one event per line, flushed
immediately. Spawn it as a subprocess and read stdout line-by-line.

Example output with `--json`:

```
{"event": "track_started", "title": "Britney Spears - Oops!...", "duration": 211.0, "ts": 1771780646}
{"event": "track_ended",   "title": "Britney Spears - Oops!...", "ts": 1771780857}
{"event": "queue_low",     "count": 1, "depth": 5, "ts": 1771780857}
{"event": "engine_stopped", "ts": 1771780900}
```

**`status [--json]`** — one-shot snapshot of current state. For the agent to check
what's playing right now, without subscribing to the event stream.

### Agent workflow

Claude's role is the **setup phase** and **event loop**. After seeding, the agent
spawns `claw-radio events --json` and reads its stdout line-by-line, reacting to
each event as it arrives.

```bash
# === SETUP PHASE (agent runs these top-to-bottom) ===

# 1. (Optional) Set up a genre-matched voice profile
#    Agent picks a YouTube clip that sounds right for the station
claw-radio tts voice add "https://youtube.com/watch?v=..." --name pop

# 2. Start the engine
claw-radio start

# 3. Build seed list via multiple targeted searches (agent generates queries)
#    Example: era-based vibe "2000s bubblegum pop"
claw-radio search "Billboard Year-End Hot 100 2000 site:wikipedia.org"
claw-radio search "Billboard Year-End Hot 100 2001 site:wikipedia.org"
# ... repeat per year, collect and deduplicate in agent context ...
claw-radio search "Now That's What I Call Music 2002 tracklist"
claw-radio search "teen pop 2000s compilation tracklist site:discogs.com"
# Agent reviews accumulated ~200 results, curates to ~120, adds known songs

#    Example: artist-based vibe "Kaytranada type stuff"
claw-radio search "Kaytranada discography site:discogs.com"
claw-radio search "Kaytranada 99.9% tracklist site:musicbrainz.org"
claw-radio search "Soulection radio tracklist 2016 2017"
claw-radio search "future bass house RnB 2015 2020 essential songs"
claw-radio search "artists similar to Kaytranada playlist"
# Agent deduplicates ~180 results, filters to vibe, adds tracks it knows

# 4. Seed when satisfied (agent decides this threshold)
claw-radio seed '[...curated 120-song list...]' --label "2000s bubblegum pop"

# 5. Subscribe to events and react
claw-radio events --json | while read -r event; do
    # track_started → inject banter
    # queue_low     → search + seed --append
    # engine_stopped → claw-radio start
done
```

### Exit codes

Yes, numeric exit codes are standard for CLI tools — they're how shell scripts and
agent frameworks check what went wrong programmatically. Both mechanisms are used:
the exit code signals the category of failure, and a human-readable message on
`stderr` explains the specific problem (e.g., which binary is missing, what the
parse error was). The agent reads stderr when it needs more context.

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Runtime error (message on stderr) |
| 2 | Bad usage / missing argument (cobra auto-generated) |
| 3 | Config not found or invalid (shows path + parse error) |
| 4 | Dependency not installed — shows binary name + install instructions |
| 5 | mpv engine not running — shows socket path, suggests `claw-radio start` |

---

## 7. Implementation details

### 7.1 `internal/config/config.go`

```go
package config

type Config struct {
    MPV     MPVConfig     `json:"mpv"`
    YtDlp   BinaryConfig  `json:"ytdlp"`
    FFmpeg  BinaryConfig  `json:"ffmpeg"`
    TTS     TTSConfig     `json:"tts"`
    Station StationConfig `json:"station"`
    Search  SearchConfig  `json:"search"`
}

type SearchConfig struct {
    SearxNGURL string `json:"searxng_url"` // default: "http://localhost:8888"
}

type MPVConfig struct {
    Socket string `json:"socket"`
    Binary string `json:"binary"`
    Log    string `json:"log"`
}

type BinaryConfig struct {
    Binary string `json:"binary"`
}

type TTSConfig struct {
    Socket         string            `json:"socket"`
    DataDir        string            `json:"data_dir"`
    FallbackBinary string            `json:"fallback_binary"`
    Voices         map[string]string `json:"voices"`
}

type StationConfig struct {
    QueueDepth int    `json:"queue_depth"`
    CacheDir   string `json:"cache_dir"`
    StateDir   string `json:"state_dir"`
}
```

`Load()` reads from the config path if it exists; if not, returns a struct with
all defaults applied. No config file = no error. Binary fields with empty strings
are resolved via `exec.LookPath`. Paths containing `~` are expanded to
`os.UserHomeDir()`.

`resolveBinary(configured, candidates...)` tries the configured value first, then
falls back to `exec.LookPath`:

```go
func resolveBinary(configured string, names ...string) string {
    if configured != "" {
        return configured
    }
    for _, name := range names {
        if p, err := exec.LookPath(name); err == nil {
            return p
        }
    }
    return ""
}
```

For the TTS fallback binary:

```go
if cfg.TTS.FallbackBinary == "" {
    switch runtime.GOOS {
    case "darwin":
        cfg.TTS.FallbackBinary = resolveBinary("", "say")
    default:
        cfg.TTS.FallbackBinary = resolveBinary("", "espeak-ng", "espeak", "festival")
    }
}
```

`runtime.GOOS` is a Go standard library constant that holds the current operating
system as a string: `"darwin"` on macOS, `"linux"` on Linux, `"windows"` on
Windows. It is evaluated at runtime (not compile time), so the same binary
uses the right OS-specific behaviour on each platform without needing separate
builds — this is why we can ship one `linux_amd64` binary that works on all
Linux distros. This is the **only** place it appears in the codebase.

---

### 7.2 `internal/mpv/client.go`

Direct Unix socket IPC client using Go's `net.DialUnix`. No `nc` subprocess.
Works identically on Linux and macOS.

```go
package mpv

type Client struct {
    conn    *net.UnixConn
    mu      sync.Mutex
    pending map[int64]chan json.RawMessage
    events  chan map[string]interface{}
    nextID  int64
}

func Dial(socketPath string) (*Client, error)                        { ... }
func (c *Client) Close() error                                       { ... }
func (c *Client) Command(args ...interface{}) error                  { ... }
func (c *Client) Get(prop string) (json.RawMessage, error)           { ... }
func (c *Client) Set(prop string, value interface{}) error           { ... }
func (c *Client) LoadFile(path, mode string) error                   { ... }
func (c *Client) InsertNext(path string) error                       { ... }
func (c *Client) PlaylistCount() (int, error)                        { ... }
func (c *Client) PlaylistPaths() ([]string, error)                   { ... }
func (c *Client) Events() <-chan map[string]interface{}               { return c.events }
func WaitForSocket(socketPath string, timeout time.Duration) error   { ... }
```

The client runs a read loop in a background goroutine that dispatches:

- **Event messages** (no `request_id`): sent to `events` channel
- **Response messages** (with `request_id`): sent to matching `pending[id]` channel

Multiple clients (controller + events command) can connect to the same mpv socket
simultaneously and each receive the full event stream independently.

`InsertNext` replicates the `mpv_insert_next` pattern from dj-audio:

```
1. LoadFile(path, "append")
2. pos = Get("playlist-pos")
3. cnt = Get("playlist-count")
4. if cnt > 1 and pos >= 0: Command("playlist-move", cnt-1, pos+1)
```

---

### 7.3 `internal/ytdlp/ytdlp.go`

```go
package ytdlp

type Candidate struct {
    ID         string
    Title      string
    Uploader   string
    Duration   float64
    ViewCount  int64
    IsLive     bool
    LiveStatus string
    WebpageURL string
}

func Search(binary, query string, n int) ([]Candidate, error) { ... }
func Score(c Candidate) int                                   { ... }
func BestCandidate(binary, query string) (*Candidate, error)  { ... }
func ResolveURL(binary, url string) (string, error)           { ... }
func Download(binary, url, outDir string) (string, error)     { ... }
func NormalizeSongKey(human string) string                    { ... }
```

**`Score()` logic** — direct port of `_score_candidate()` from dj-audio:

- +70: "provided to youtube" in title or uploader ends with " - topic"
- +45: "vevo" in uploader
- +40: "official audio" in title
- +10: "official" in title without "video"
- +8: "audio" in title
- -30 each: live, cover, karaoke, lyrics, lyric, remix, mix, sped up, slowed, reverb, 8d, nightcore, instrumental, extended, 1 hour, full album, playlist
- -80: duration < 90s
- -50: duration > 8 min
- +12: duration 120–420s
- -120: `is_live` = true
- -60: `live_status` not in ("not_live", "")
- +6/+10/+14/+18: view count tiers (50k / 500k / 5M / 50M)
- -(len(title)-60)/10: penalty for very long titles

---

### 7.4 `internal/station/station.go`

Station state management. Files in `<state_dir>/`:

**`station.json`** — seed list and optional display label:

```json
{
  "label": "2000s bubblegum pop",
  "seeds": ["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]
}
```

Seeds are the song strings provided by the agent via `seed`. The controller wraps
each in a `ytsearch10:... official audio` query for yt-dlp.

**`state.json`** — runtime state persisted across restarts:

```json
{
  "played_video_ids": [],
  "played_song_keys": []
}
```

De-duplication tracks played songs so they don't repeat within a session.

---

### 7.5 `internal/station/controller.go`

Event-driven station controller — the long-running background daemon.

**What it does:** connects to mpv, watches for `end-file` events, and fills the
queue by downloading the next song from the seed list.

```go
func (s *svc) run() {
    // 1. Load station.json + state.json
    // 2. Connect to mpv IPC (retry loop with backoff)
    // 3. fillQueue() immediately
    // 4. Start 30s safety-net ticker

    for {
        select {
        case event := <-mpvClient.Events():
            if event["event"] == "end-file" {
                fillQueue()
            }
        case <-ticker.C:
            fillQueue() // safety net; end-file is primary trigger
        case <-stopCh:
            saveState()
            return
        }
    }
}
```

**`fillQueue()`:**

```go
func (s *svc) fillQueue() {
    count, _ := mpvClient.PlaylistCount()
    for count < s.cfg.Station.QueueDepth {
        seed := s.pickSeed()  // round-robin, skip already-played
        candidate, _ := ytdlp.BestCandidate(
            s.cfg.YtDlp.Binary,
            "ytsearch10:"+seed+" official audio",
        )
        if s.state.alreadyPlayed(candidate.ID) { continue }
        path, _ := ytdlp.Download(...)
        mpvClient.LoadFile(path, "append")
        s.state.markPlayed(candidate.ID, ytdlp.NormalizeSongKey(candidate.Title))
        count++
    }
}
```

The controller is deliberately dumb — it fills the queue and nothing else.
Banter, Telegram, and seed discovery are the agent's job.

**Background process management:**

The controller is launched as a background process by `claw-radio start`:

```go
cmd := exec.Command(os.Executable(), "station", "daemon")
cmd.Stdout = logFile
cmd.Stderr = logFile
cmd.Start()
writePID("/tmp/claw-radio-controller.pid", cmd.Process.Pid)
```

`claw-radio stop` reads the PID file and sends SIGTERM. No systemd, no LaunchAgent,
no `kardianos/service`. Simple and cross-platform.

---

### 7.6 `cmd/play.go` — play behavior

`claw-radio play <query|url>` — insert at front, play immediately:

```
1. Resolve query to a local file path via yt-dlp (download)
2. Call mpvClient.InsertNext(path)  → puts file right after current track
3. Call mpvClient.Command("playlist-next") → skips current, plays our file
```

The queue behind the insertion point is preserved. The controller sees the
`end-file` event when the current skips out and refills normally.

`claw-radio queue <query|url>` — standard append:

```
1. Resolve + download
2. Call mpvClient.LoadFile(path, "append")
```

---

### 7.7 `cmd/say.go` + `cmd/tts.go` — agent-driven banter + voice profiles

#### Voice profiles via yt-dlp

`claw-radio tts voice add <url> [--name <name>]`

The agent matches the voice to the station genre by downloading a real reference clip:

```
1. yt-dlp -x --audio-format wav -o "<data_dir>/voices/<name>.%(ext)s" <url>
   (uses ffmpeg for conversion)
2. Trim to 30s if longer (Chatterbox works best with 10–30s reference)
3. Print: "Voice 'pop' saved → ~/.local/share/claw-radio/voices/pop.wav"
```

No config file modification needed. The voice is discoverable by name from
`<data_dir>/voices/<name>.wav` automatically.

The agent searches YouTube for a suitable voice clip. e.g.:

- Station vibe "country" → search "hillbilly radio DJ sample voice"
- Station vibe "electronic" → search "German techno radio announcer clip"
- Station vibe "pop" → search "California valley girl radio host promo"

#### Voice resolution in `cmd/say.go`

`claw-radio say "<text>" [--voice <name|/path/to.wav>]`

The `--voice` flag accepts either a name or a file path:

```go
func resolveVoicePath(name string, cfg *config.Config) string {
    if name == "" { name = "default" }
    // 1. Explicit config map entry
    if p, ok := cfg.TTS.Voices[name]; ok && p != "" { return p }
    // 2. <data_dir>/voices/<name>.wav (auto-discovered after tts voice add)
    candidate := filepath.Join(cfg.TTS.DataDir, "voices", name+".wav")
    if _, err := os.Stat(candidate); err == nil { return candidate }
    // 3. Literal file path
    if _, err := os.Stat(name); err == nil { return name }
    return "" // no prompt → Chatterbox uses its built-in default voice
}
```

```
1. Resolve voice WAV path via resolveVoicePath()
2. Generate output path: <data_dir>/banter/<timestamp>.wav
3. Call tts.Render(text, voicePath, outPath)
4. Call mpvClient.InsertNext(outPath)
5. Print: "queued banter"
```

Returns exit 0 when the file is queued. mpv plays it when the current track ends.

---

### 7.8 `cmd/events.go` — JSON event stream

`claw-radio events [--json]`

Long-running command. Connects to mpv IPC, translates mpv events to our schema,
and writes to stdout. The agent runs this as a subprocess and reads line-by-line.

This is the same pattern as `docker events` or `kubectl get --watch`. It's how
the agent reacts immediately (no polling) to: track changes, queue running low,
engine stopping.

```go
for event := range client.Events() {
    switch event["event"] {
    case "file-loaded":
        title, _ := client.Get("media-title")
        dur, _   := client.Get("duration")
        emit(Event{Type: "track_started", Title: asString(title), Duration: asFloat(dur)})
    case "end-file":
        emit(Event{Type: "track_ended", Title: lastTitle})
        count, _ := client.PlaylistCount()
        if count <= 2 {
            emit(Event{Type: "queue_low", Count: count, Depth: cfg.Station.QueueDepth})
        }
    }
}
emit(Event{Type: "engine_stopped"})
```

Without `--json`, output is human-readable text (for interactive debugging).

---

### 7.9 `internal/tts/tts.go`

Three-layer TTS chain, called by `cmd/say.go`.

```go
type Client struct{ cfg *config.Config }

func (c *Client) Render(text, genre string, outPath string) error { ... }
```

**Layer 1 — Warm Chatterbox daemon** (preferred):
Connect to `cfg.TTS.Socket`, send JSON request with optional `voice_prompt`.
Falls through if socket connection is refused (daemon not running).

**Layer 2 — One-shot Chatterbox** (fallback if daemon unavailable):
Exec `<data_dir>/venv/bin/python <data_dir>/daemon.py --one-shot ...`.
Falls through if venv is not present.

**Layer 3 — System TTS** (zero-install default):

- macOS: `say -o <out.aiff> "<text>"` — always available, ships with macOS
- Linux: `espeak-ng -w <out.wav> "<text>"` — **not pre-installed** on most distros

`espeak-ng` must be installed separately on Linux:

```
Debian/Ubuntu: apt install espeak-ng
Fedora/RHEL:   dnf install espeak-ng
Arch:          pacman -S espeak-ng
```

If neither Chatterbox nor a system TTS binary is found, `claw-radio say` exits
with code 4 and prints:

```
No TTS binary found.
Install Chatterbox:  claw-radio tts install
Or system TTS:       apt install espeak-ng  (Linux)  |  ships with macOS
```

Selected via `cfg.TTS.FallbackBinary` (set by config loader using `runtime.GOOS`).
If `espeak-ng` is not found via `exec.LookPath`, the field stays empty and layer 3
is unavailable — layers 1 and 2 still work if Chatterbox is installed.

System TTS is instant (no ML model). It sounds robotic but requires zero setup
once the binary is installed.

---

### 7.10 `tts/daemon.py` (embedded Python)

Warm Chatterbox daemon on Unix socket. Architecture unchanged from dj-audio, plus:

1. **Voice prompt support**: request JSON gains optional `"voice_prompt"` field.
   When present, Chatterbox clones that voice:

   ```python
   wav = model.generate(text, audio_prompt_path=voice_prompt_path)
   ```

   `voice_prompt_path` must be a WAV file — 10–30 seconds of clean speech gives
   best results. Sources:
   - Bundled reference WAVs: `tts/voices/pop.wav`, `country.wav`, `electronic.wav`
     (included in the binary via `go:embed`, extracted by `tts install`)
   - Agent-downloaded references: `claw-radio tts voice add <url>` puts WAVs in
     `<data_dir>/voices/` — the same directory, accessible by name

   If `voice_prompt_path` is not set, Chatterbox generates in its built-in default
   voice (neutral American English — sounds like a real person, just not customized).

2. **Cross-platform GPU detection**:

```python
def get_device():
    import torch
    if torch.backends.mps.is_available():
        return "mps"      # Apple Silicon
    if torch.cuda.is_available():
        return "cuda"     # Linux NVIDIA
    return "cpu"          # fallback
```

1. **One-shot mode**: `python daemon.py --one-shot <text> <out_path> [--voice <wav>]`

---

### 7.11 `internal/search/` — SearxNG web scraper

Single-responsibility package: take a raw query string, return a deduplicated list
of (artist, title) pairs. No query generation logic lives here — that is the agent's
job, guided by SKILL.md.

#### `internal/search/search.go`

```go
package search

type Result struct {
    Artist string `json:"artist"`
    Title  string `json:"title"`
}

type Client struct {
    searxngURL string
    http       *http.Client
}

func NewClient(searxngURL string) *Client {
    return &Client{
        searxngURL: searxngURL,
        http:       &http.Client{Timeout: 30 * time.Second},
    }
}

// Search submits one query to SearxNG, fetches the top result pages,
// runs site-specific extractors, deduplicates, and returns up to maxResults pairs.
func (c *Client) Search(query string, maxResults int) ([]Result, error) {
    urls, err := c.fetchURLs(query, 6) // top 6 SearxNG results
    if err != nil {
        return nil, fmt.Errorf("searxng unreachable at %s: %w", c.searxngURL, err)
    }
    var all []Result
    seen := map[string]bool{}
    for _, u := range urls {
        results, err := c.fetchAndExtract(u)
        if err != nil {
            continue // skip unreachable pages silently
        }
        for _, r := range results {
            key := strings.ToLower(r.Artist + "||" + r.Title)
            if !seen[key] {
                seen[key] = true
                all = append(all, r)
            }
        }
        if len(all) >= maxResults {
            break
        }
    }
    if len(all) > maxResults {
        all = all[:maxResults]
    }
    return all, nil
}

// fetchURLs queries SearxNG JSON API and returns the top N result URLs.
func (c *Client) fetchURLs(query string, n int) ([]string, error) {
    u := c.searxngURL + "/search?q=" + url.QueryEscape(query) + "&format=json"
    // ... net/http GET, unmarshal results[].url ...
}

// fetchAndExtract fetches a URL and dispatches to the correct extractor.
func (c *Client) fetchAndExtract(rawURL string) ([]Result, error) {
    html, err := c.fetchPage(rawURL)
    if err != nil {
        return nil, err
    }
    u := strings.ToLower(rawURL)
    switch {
    case strings.Contains(u, "wikipedia.org"):
        return extract.Wikitable(html), nil
    case strings.Contains(u, "discogs.com"):
        return extract.Discogs(html), nil
    case strings.Contains(u, "musicbrainz.org"):
        return extract.MusicBrainz(html), nil
    default:
        return extract.Generic(html), nil
    }
}
```

#### `internal/search/extract.go`

Four extractors, direct ports of the Python implementations in `deep_research.py`:

```go
package extract

// Result holds a single extracted (artist, title) pair.
type Result struct {
    Artist string
    Title  string
}

// Wikitable parses Wikipedia chart pages which use <table class="wikitable">.
//
// Wikipedia chart articles (e.g. "Billboard Year-End Hot 100 singles of 2001",
// "UK Singles Chart") have tables where each row is one chart position.
// Column headers vary but follow consistent patterns.
//
// Algorithm:
//   1. Find all <table class="wikitable"> elements using regex/string search.
//   2. For each table, read the first <tr> (header row) to identify column indices.
//      Artist columns: header text matches "Artist", "Act", "Performer", "Artist(s)".
//      Title columns:  header text matches "Single", "Title", "Song", "Song title".
//   3. For each subsequent <tr> (data row), extract inner text from the cells at
//      the identified artist and title column indices.
//      Strip footnote markers like "[12]" using a simple regex: \[\d+\]
//      Strip surrounding whitespace.
//   4. Skip rows where either field is empty.
//   5. Return one Result per valid row.
//
// Accuracy: high. Wikipedia chart tables are machine-generated and consistent.
// Yield: 50–200 results per page (one per chart position).
func Wikitable(html string) []Result { ... }

// Discogs parses compilation/album tracklist pages on discogs.com.
//
// Discogs renders every track in a structured table with CSS classes:
//   - Artist cell: <span class="tracklist_track_artists"> (one or more artist links)
//   - Title cell:  <span class="tracklist_track_title">
//
// Algorithm:
//   1. Find all elements with class "tracklist_track_artists". Extract inner text
//      (strip HTML tags, trim). For VA compilations each track has its own artist.
//      For artist albums, look for a page-level artist in <meta property="og:title">
//      as fallback (format: "Artist – Album Title", split on " – ").
//   2. Find all elements with class "tracklist_track_title". Extract inner text.
//   3. Pair by index: artists[i] → titles[i]. The two lists are equal-length on
//      well-formed Discogs pages. If lengths differ, zip up to min(len) and discard
//      extras.
//   4. Return one Result per pair where both fields are non-empty.
//
// Accuracy: high. Discogs markup is consistent and semantically annotated.
// Yield: 8–50 results per page (one per track on the release).
func Discogs(html string) []Result { ... }

// MusicBrainz parses release/tracklist pages on musicbrainz.org.
//
// MusicBrainz is a structured music database. Release pages list tracks in a
// table where each row is one track.
//
// Algorithm:
//   1. Extract the release artist from <meta property="og:title">. The format is
//      "Artist Name – Release Title" (em-dash). Split on " – " and take the left
//      part. If og:title is absent, fall back to the first <h1> or <title> element.
//   2. Find all <td class="title"> elements. Extract inner text of each (the track
//      title). MusicBrainz title cells may contain links — extract the link text.
//   3. For "Various Artists" releases, attempt to read a per-row artist from the
//      <td class="artist"> cell in the same <tr>. If present, use it; otherwise
//      use the release artist from step 1.
//   4. Return one Result per title where the title field is non-empty.
//
// Accuracy: high. MusicBrainz is a curated database with strict structured markup.
// Yield: 5–30 results per page (one per track).
func MusicBrainz(html string) []Result { ... }

// Generic extracts song pairs from arbitrary HTML pages using regex heuristics.
// Used for editorial listicles, best-of articles, and any site not matched above.
//
// Algorithm:
//   1. Strip all HTML tags (replace with spaces) to get plain text.
//   2. Split into lines. For each non-empty line (trimmed), try three patterns
//      in order (first match wins):
//      a. Em-dash pattern:   "ARTIST – TITLE" or "ARTIST — TITLE"
//         (Unicode U+2013 or U+2014; common in editorial writing)
//      b. Hyphen pattern:    "ARTIST - TITLE"
//         Only match when both sides have 2–80 characters and at least one space
//         (avoids matching plain-text timestamps like "3:45 - 4:12").
//      c. "By" pattern:      "TITLE by ARTIST" (case-insensitive)
//         Require "by" as a whole word, both sides 2–60 characters.
//   3. Filter out results where either field contains noise keywords (case-insensitive):
//      "lyrics", "karaoke", "nightcore", "instrumental", "cover", "remix", "mix",
//      "1 hour", "extended", "sped up", "slowed", "reverb", "8d audio", "mashup",
//      "full album", "playlist", "megamix", "medley", "tribute".
//   4. Discard results where either field is longer than 80 chars or shorter than
//      2 chars (likely parse garbage).
//   5. Return one Result per passing pair.
//
// Accuracy: medium. Expect 10–30% noise. The agent's curation step handles this.
// Yield: varies widely — 0 to 100 results per page.
func Generic(html string) []Result { ... }
```

<comment>Describer these extractors more complete in code, the one building the project doesn't know the origonal code</comment>

**Extractor accuracy by source** (important for the agent's expectations):

| Source | Accuracy | Why |
|---|---|---|
| Wikipedia wikitable | High | Structured HTML tables, consistent columns |
| Discogs tracklist | High | Semantic CSS classes per field |
| MusicBrainz | High | Structured release database pages |
| Generic fallback | Medium | Regex over prose text — produces some noise |

The agent should expect the generic extractor to produce some false positives and
factor that into its curation step.

#### `cmd/search.go`

```go
// claw-radio search "<query>"
// stdout: JSON array of "Artist - Title" strings (pipe-friendly)
// stderr: human-readable summary (pages fetched, songs found)

func runSearch(cmd *cobra.Command, args []string) error {
    query := args[0]
    client := search.NewClient(cfg.Search.SearxNGURL)
    results, err := client.Search(query, 150)
    if err != nil {
        return exitCode(err, 1)
    }
    // Format as simple strings matching the seed command's input format
    songs := make([]string, len(results))
    for i, r := range results {
        songs[i] = r.Artist + " - " + r.Title
    }
    // stdout: clean JSON for piping
    json.NewEncoder(os.Stdout).Encode(songs)
    // stderr: human summary
    fmt.Fprintf(os.Stderr, "Fetched pages, extracted %d unique songs.\n", len(songs))
    return nil
}
```

The output format matches `seed`'s input format exactly — the agent can pass the
output straight through or modify it before seeding.

---

### 7.12 `internal/provider/` — Audio Provider interface

A Provider takes a seed string (e.g. `"Britney Spears - Oops I Did It Again"`)
and returns a local audio file path ready for mpv. **Discovery is always the
agent's job** — the Provider's only concern is: given a song name, get me audio.

This makes it easy to swap the audio backend:
- **Today**: YouTube via yt-dlp
- **Future**: Spotify (direct stream), Apple Music, local file library

```go
package provider

// Provider resolves a song seed into a local audio file.
// The seed is typically "Artist - Title" but may also be a direct URL.
type Provider interface {
    // Resolve downloads (or locates) audio for the seed string and returns
    // the path to a local file ready for mpv. The cacheDir is where
    // downloaded files should be stored.
    Resolve(seed, cacheDir string) (audioPath string, err error)

    // Name returns a short identifier used in logs ("youtube", "spotify", etc.)
    Name() string
}
```

#### `ytdlp.go` — YouTube provider (current default)

```go
// YtDlpProvider resolves songs by searching YouTube and downloading the best match.
// It is the default provider. Uses the scoring algorithm from internal/ytdlp
// to pick the best candidate from search results.
type YtDlpProvider struct {
    Binary string // path to yt-dlp binary
    FFmpeg string // path to ffmpeg binary (used by yt-dlp for audio extraction)
}

func (p *YtDlpProvider) Resolve(seed, cacheDir string) (string, error) {
    // 1. Search: ytdlp.BestCandidate(p.Binary, "ytsearch10:"+seed+" official audio")
    //    Returns the highest-scoring Candidate struct (see internal/ytdlp scoring).
    // 2. Check if already cached: cacheDir/<video_id>.opus (skip download if exists)
    // 3. Download: ytdlp.Download(p.Binary, candidate.WebpageURL, cacheDir)
    // 4. Return the local file path.
}

func (p *YtDlpProvider) Name() string { return "youtube" }
```

#### `spotify.go` — Spotify provider (stub, not implemented)

```go
// SpotifyProvider is a future provider that sources audio via the Spotify API.
// When implemented: uses Spotify Web API to get 30-second preview URLs
// (free tier) or full track streams (premium). Eliminates yt-dlp dependency
// for users with a Spotify subscription.
type SpotifyProvider struct {
    ClientID     string
    ClientSecret string
}

func (p *SpotifyProvider) Resolve(seed, cacheDir string) (string, error) {
    return "", fmt.Errorf("spotify provider not implemented")
}

func (p *SpotifyProvider) Name() string { return "spotify" }
```

#### `applemusic.go` — Apple Music provider (stub, not implemented)

```go
// AppleMusicProvider is a future provider that sources audio via Apple Music API.
type AppleMusicProvider struct{}

func (p *AppleMusicProvider) Resolve(seed, cacheDir string) (string, error) {
    return "", fmt.Errorf("apple music provider not implemented")
}

func (p *AppleMusicProvider) Name() string { return "applemusic" }
```

The controller uses the `Provider` interface — it never calls yt-dlp directly:

```go
type svc struct {
    provider provider.Provider
    // ...
}

func (s *svc) fillQueue() {
    seed := s.pickSeed()
    path, err := s.provider.Resolve(seed, s.cfg.Station.CacheDir)
    if err != nil { /* log and skip */ return }
    mpvClient.LoadFile(path, "append")
}
```

`YtDlpProvider` is instantiated in `cmd/start.go` and passed to the controller.
Swapping to Spotify in the future = instantiate `SpotifyProvider` instead.

---

### 7.12 `cmd/start.go` — engine management

`claw-radio start`:

1. Check mpv and yt-dlp are found. If missing, exit 4 with platform-specific
   install instructions (both options printed regardless of OS — let the user
   pick the right one):

   ```
   mpv not found.
   Install on macOS:  brew install mpv
   Install on Linux:  apt install mpv   (Debian/Ubuntu)
                      dnf install mpv   (Fedora)
   ```

2. Kill any existing mpv on the socket path (send `quit`, best-effort)
3. Launch mpv as background process:

   ```
   mpv --no-video --idle=yes --force-window=no --audio-display=no
       --cache=yes --cache-secs=20
       --demuxer-max-bytes=50MiB
       --input-ipc-server=<cfg.MPV.Socket>
   ```

4. Write PID to `/tmp/claw-radio-mpv.pid`
5. `mpv.WaitForSocket(cfg.MPV.Socket, 5*time.Second)`
6. Launch controller: `exec.Command(os.Executable(), "station", "daemon")`, write PID
7. Start Chatterbox daemon if venv exists; skip silently if not
8. Print: `claw-radio started`

Volume is the user's concern — mpv starts at its own default. The tool never
touches volume.

`claw-radio stop`:

1. Read PIDs from `/tmp/claw-radio-*.pid` and send SIGTERM to each
2. Send `{"command":["quit"]}` to mpv IPC (graceful quit)
3. Remove socket files and PID files

No systemd, no LaunchAgent, no `kardianos/service`. Works identically on both
platforms. The radio lives as long as the processes are alive; the agent restarts
it when needed.

---

### 7.13 `cmd/status.go` — JSON schema

```json
{
  "engine": "running",
  "station": {
    "label": "2000s bubblegum pop",
    "seeds": 48
  },
  "playback": {
    "state":    "playing",
    "title":    "Britney Spears - Oops!... I Did It Again",
    "time_pos": 47.3,
    "duration": 211.0,
    "volume":   30
  },
  "queue": {
    "count": 4,
    "depth": 5
  },
  "controller": "running",
  "tts":        "warm"
}
```

`tts` field: `"warm"` if Chatterbox daemon socket responds, `"system"` if only
system TTS is available, `"unavailable"` if no TTS binary found.

---

### 7.14 Exit code helper

```go
type exitError struct{ err error; code int }
func (e *exitError) Error() string { return e.err.Error() }
func exitCode(err error, code int) *exitError { return &exitError{err, code} }
```

In `main.go`, type-assert error from `Execute()` to `*exitError` and call
`os.Exit(code)`.

---

## 8. Cache and GC

**Song cache** (`cfg.Station.CacheDir`):
Delete any cached files not in mpv's current playlist, keeping the `queue_depth + 3`
most recently downloaded. Implemented with `os.ReadDir` + `os.Remove`.

---

## 9. GoReleaser configuration

```yaml
version: 2

project_name: claw-radio

changelog:
  disable: true

builds:
  - env:
      - CGO_ENABLED=0
    targets:
      - darwin_amd64
      - darwin_arm64
      - linux_amd64
      - linux_arm64
    ldflags:
      - -s -w -X main.version={{.Version}}
    binary: claw-radio

universal_binaries:
  - replace: true

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - config.example.json
      - README.md

checksum:
  name_template: "checksums.txt"

homebrew_casks:
  - name: claw-radio-cli
    binaries:
      - claw-radio
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/claw-radio"]
          end
    repository:
      owner: vossenwout
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: https://github.com/vossenwout/claw-radio
    description: "GTA-style AI radio station CLI — operated by an AI agent"
    caveats: |
      Install required tools:
        brew install mpv yt-dlp ffmpeg

      Optional (high-quality TTS voice):
        claw-radio tts install
```

### Release workflow

```yaml
name: Release
on:
  push:
    tags: ['v*']
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: '1.25', cache: true }
      - uses: goreleaser/goreleaser-action@v6
        with: { version: latest, args: release --clean }
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

### Linux install

```bash
curl -L https://github.com/vossenwout/claw-radio/releases/latest/download/claw-radio_linux_amd64.tar.gz \
  | tar -xz -C ~/.local/bin

# Optional: high-quality TTS
claw-radio tts install
```

---

## 10. Testing approach

### Philosophy

Unit tests mock all subprocess and network calls. `go test ./...` must pass on
any platform with no external tools installed.

### Mock pattern

```go
// internal/ytdlp/ytdlp.go
var execCommand = exec.Command

// internal/tts/tts.go
var execCommand = exec.Command
```

Tests replace these before calling the function under test and restore with `defer`.

### What each package tests

**`internal/config`** — temp files:

- No config file → returns all defaults, no error
- Valid JSON loads, defaults applied, `~` expanded
- Empty binary strings resolved via LookPath mock
- `runtime.GOOS` → correct `FallbackBinary` default

**`internal/mpv`** — mock Unix socket server:

- `Dial` errors when socket absent
- `Get` returns correct value for property
- `InsertNext` moves item to correct playlist position
- `WaitForSocket` times out cleanly

**`internal/ytdlp`** — mock `execCommand`:

- `Score` table-driven: official audio, live video, short clip, high view count
- `NormalizeSongKey` strips noise, lowercases
- `BestCandidate` picks highest-scoring from mocked yt-dlp JSON

**`internal/station`** — temp directories:

- `Load` / `Save` roundtrip for `station.json` and `state.json`
- `SetSeeds` overwrites; `AppendSeeds` adds without duplication
- `MarkPlayed` deduplication works correctly

**`internal/tts`** — mock socket + `execCommand`:

- Tries daemon socket first; falls back on refused connection
- Falls back to one-shot on daemon unavailable
- Falls back to system TTS when venv missing

**`internal/search`** — mock HTTP server:

- `fetchURLs` parses SearxNG JSON response and returns URLs
- `Wikitable` extracts correct pairs from a saved Wikipedia chart HTML fixture
- `Discogs` extracts correct pairs from a saved Discogs tracklist HTML fixture
- `MusicBrainz` extracts correct pairs from a saved MusicBrainz HTML fixture
- `Generic` extracts "Artist - Title" patterns and filters junk (lyrics, karaoke, etc.)
- `Search` deduplicates across extractors, caps at `maxResults`
- `Search` returns clear error when SearxNG is unreachable (mock timeout)

**`internal/provider`** — mock `execCommand`:

- `YtDlpProvider.Resolve` calls `ytdlp.BestCandidate` + `ytdlp.Download` correctly
- `YtDlpProvider.Resolve` returns cached file path when file already exists in cacheDir
- Stub providers return "not implemented" cleanly

### Table-driven test example

```go
func TestScore(t *testing.T) {
    tests := []struct {
        name        string
        c           ytdlp.Candidate
        wantAtLeast int
    }{
        {"official audio",  ytdlp.Candidate{Title: "Song (Official Audio)", Duration: 200}, 40},
        {"live video",      ytdlp.Candidate{Title: "Song Live at VMAs", IsLive: true}, -120},
        {"too short",       ytdlp.Candidate{Title: "Clip", Duration: 60}, -80},
        {"vevo high views", ytdlp.Candidate{Uploader: "ArtistVEVO", ViewCount: 10_000_000, Duration: 210}, 50},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := ytdlp.Score(tt.c); got < tt.wantAtLeast {
                t.Errorf("Score() = %d, want >= %d", got, tt.wantAtLeast)
            }
        })
    }
}
```

### Integration tests

Behind `//go:build integration`. Require actual hardware. Never run in CI:

```
go test -tags=integration ./...
```

---

## 11. CI

```yaml
name: CI
on:
  push:    { branches: [main] }
  pull_request: { branches: [main] }

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod, cache: true }
      - name: Format check
        run: if [ -n "$(gofmt -l .)" ]; then gofmt -l .; exit 1; fi
      - name: Vet
        run: go vet ./...
      - name: Test
        run: go test ./...
```

---

## 12. Platform summary

| Concern | macOS | Linux | How handled |
|---|---|---|---|
| Playback engine | mpv | mpv | Identical — mpv IPC is the same |
| IPC socket | Unix socket | Unix socket | `net.DialUnix` — identical |
| Volume | mpv default | mpv default | User's concern — tool never sets it |
| Background processes | PID files | PID files | `os/exec` + `/tmp/*.pid` — identical |
| TTS GPU | MPS (Apple Silicon) | CUDA / CPU | daemon.py checks both |
| TTS fallback | `say` (always present) | `espeak-ng` (needs `apt install`) | Detected in `config.Load()`; clear error if missing |
| Binary paths | Homebrew paths | `/usr/bin`, apt paths | `exec.LookPath` — identical |
| Go binary | `darwin_universal` | `linux_amd64`, `linux_arm64` | GoReleaser |
| Web search | SearxNG (HTTP) | SearxNG (HTTP) | `internal/search` → same `net/http` on both |
| Song discovery | Agent + `search` | Agent + `search` | Agent generates queries, tool scrapes, agent curates |
| Audio provider | `YtDlpProvider` | `YtDlpProvider` | `internal/provider` interface — swap for Spotify/Apple Music later |
| Banter text | Agent generates | Agent generates | Agent calls `say` with text |
| Notifications | Agent handles | Agent handles | Agent reads `events` stream |

**`runtime.GOOS` appears in exactly one place**: `config.Load()`, to pick the
TTS fallback binary (`say` vs `espeak-ng`).

---

## 13. `SKILL.md` design

The SKILL.md does two things: (1) gives the agent its radio host persona and
(2) teaches it how to operate the CLI machinery. The tool has no personality or
intelligence — both live entirely in SKILL.md.

```markdown
---
name: claw-radio
description: >
  GTA-style AI radio station. You operate the radio as a character — a GTA-style
  radio host whose voice and energy matches the station vibe. The CLI is your
  control board; you are the host. Use this skill to: start the radio, build a
  song seed list by searching the web, inject spoken banter between tracks (you
  write the script, the tool speaks it), and react to playback events. Works on
  macOS and Linux. Requires: mpv, yt-dlp, SearxNG.

---

## Persona

You are a GTA-style radio host. Stay in character at all times while the radio
is running. Match your voice and energy to the station vibe:

- **pop / bubblegum**: bubbly California valley energy, 25-year-old woman,
  genuinely excited, lots of exclamation marks
- **country / americana**: southern drawl, folksy, slightly self-deprecating,
  talks like the songs mean something real
- **electronic / techno**: dry German efficiency, minimal emotion, connoisseur,
  treats every track like a rare artifact
- **hip-hop / rap**: confident, street-smart, New York energy, cultural authority
- **rock / alternative**: world-weary, slightly sarcastic, classic rock veteran
  who's seen everything twice
- **jazz / soul**: smooth, unhurried, knows every musician's real name
- **default**: dry, deadpan, absurdist GTA radio host — slightly above it all

Banter is short: 1–2 sentences, under 25 words. Dry, specific, never generic.
Avoid "Welcome to [station name]!" Every time. Vary what you say.

---

## Building a seed list

Before seeding, call `claw-radio search` multiple times to build a good song list.
Each call executes ONE query against SearxNG and returns up to 150 songs as JSON.
You accumulate results across calls, deduplicate in your context, and seed when
satisfied.

**Deciding when you have enough:**
- 100+ unique songs that feel representative of the vibe
- Good mix of well-known and deeper cuts
- Not just one artist's discography unless that's the explicit request
- If not satisfied → generate more queries and search again

**After searching:**
Always supplement with 10–20 songs from your own knowledge that are central to
the vibe but didn't appear. Remove obvious outliers. Then seed.

### Era / genre vibes  ("90s hip hop", "2000s bubblegum pop", "80s synth-pop")

Search year-by-year chart pages — these give real chart data and high accuracy:

```

claw-radio search "Billboard Year-End Hot 100 <year> site:wikipedia.org"
claw-radio search "UK Singles Chart <year> year end site:wikipedia.org"
claw-radio search "Now That's What I Call Music <year> tracklist"
claw-radio search "<genre> <decade> compilation tracklist site:discogs.com"
claw-radio search "best <genre> songs of the <decade>s"

```

Repeat the Billboard/UK queries for each year in the decade. This typically yields
100–150 songs per year from structured Wikipedia tables (high accuracy).

### Artist-based vibes  ("Kaytranada type", "sounds like Daft Punk", "J Dilla influenced")

Use the anchor artist to triangulate the sonic space — their discography, scene,
collaborators, and associated genre:

```

claw-radio search "<artist> discography site:discogs.com"
claw-radio search "<artist> tracklist site:musicbrainz.org"
claw-radio search "artists similar to <artist> playlist"
claw-radio search "<artist> DJ set tracklist"         # surfaces related artists
claw-radio search "<associated genre> essential songs"
claw-radio search "<collaborator 1> discography site:discogs.com"
claw-radio search "<collaborator 2> discography site:discogs.com"

```

Artist-based vibes require more queries (5–10) and more curation, since you're
building a genre space around one artist rather than pulling from charts.

### Mood / abstract vibes  ("late night driving", "rainy Sunday", "gym motivation")

Map the mood to genres and eras first, then search those:

```

# Think: "late night driving" → synthwave, 80s new wave, lo-fi house, future bass

claw-radio search "synthwave essential songs site:discogs.com"
claw-radio search "80s new wave best songs list"
claw-radio search "lo-fi house playlist tracklist"

```

---

## Full startup flow

```bash
# 0. (Optional) Download a voice profile matched to the station genre.
#    Find a YouTube clip that sounds right — real radio DJ, character, etc.
claw-radio tts voice add "https://youtube.com/watch?v=..." --name country

# 1. Start the engine
claw-radio start

# 2. Search — run multiple queries, accumulate results
claw-radio search "<query 1>"   # → collect results
claw-radio search "<query 2>"   # → accumulate, deduplicate
claw-radio search "<query 3>"   # → keep going until satisfied (100+ songs)

# 3. Seed when satisfied (agent curates + supplements from own knowledge)
claw-radio seed '[...curated list...]' --label "<vibe>"

# 4. Subscribe to events
claw-radio events --json | while read -r event; do
    # react based on event.type — see "Reacting to events" below
done
```

## Reacting to events

After seeding, spawn `claw-radio events --json` and read its stdout. Each line
is one JSON event. React, keep reading.

**`track_started`** — inject banter before every song:

- Always say something — every track gets a line
- Write in your persona — short (1–2 sentences, under 25 words), specific, in the moment
- `claw-radio say "<quip>" --voice <genre>`

**`queue_low`** — run more searches and append:

- `claw-radio search "<another query>"` → accumulate new results
- `claw-radio seed '[new songs]' --append`

**`track_ended`** — nothing required.

**`engine_stopped`** — the engine quit unexpectedly; restart with `claw-radio start`.

## Stopping

```bash
claw-radio stop
```

---

```

---

## 14. Known limitations

**TTS on a headless Linux VPS without GPU:**
Chatterbox on CPU takes 10–30s per banter clip. Don't run `claw-radio tts install`
on VPS; install `espeak-ng` instead (instant, no ML).

**Chatterbox + CUDA on Linux:**
Default `torch` pip wheel is CPU. Install the CUDA wheel manually before
`claw-radio tts install`.

**Voice cloning reference quality:**
`tts voice add` trims to 30s. Background noise or music in the reference will
bleed into generated speech. Look for clean spoken-word clips on YouTube.

**yt-dlp drift:**
YouTube's internal API occasionally breaks yt-dlp. Run `pip install -U yt-dlp`
or `brew upgrade yt-dlp` if songs stop resolving.

**No auto-restart on reboot:**
The radio runs as a plain background process, not a registered system service.
The agent restarts it when needed. (Service management can be added later if
persistent operation across reboots becomes a requirement.)

**Voice cloning quality:**
Requires a 10–30s clean reference WAV. Replace files in `<data_dir>/voices/`
for better results.

---

## 15. Hardcoded values eliminated

| Was hardcoded in dj-audio | Now in |
|---|---|
| All `/Users/spookie/...` paths | Config defaults (`~/.local/share/claw-radio/`) |
| `/tmp/spookie-mpv.sock` | `config.mpv.socket` (auto-default) |
| `/tmp/spookie-chatterbox.sock` | `config.tts.socket` (auto-default) |
| `CHAT_ID = "1977937165"` | Agent (reads from `events` stream) |
| `/opt/homebrew/bin/yt-dlp` | `exec.LookPath` auto-detect |
| `/opt/homebrew/bin/ffmpeg` | `exec.LookPath` auto-detect |
| `osascript` volume control | Out of scope — user's concern, mpv starts at its own default |
| LaunchAgent plist | Plain background process + PID file |
| Static banter pool | Agent generates banter text (every song) |
| Single TTS voice for all genres | `config.tts.voices` map + `tts voice add` (agent downloads via yt-dlp) |
| SearxNG hardcoded to OpenClaw paths | `config.search.searxng_url` (default `http://localhost:8888`) |
| Query generation hardcoded in Python | Agent generates queries; SKILL.md guides the strategy |
| YouTube hardcoded as only audio source | `internal/provider` interface — `YtDlpProvider` today, Spotify/Apple Music tomorrow |

---
