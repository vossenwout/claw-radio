# Research: dj-audio radio feature

This documents the full radio implementation inside the `dj-audio` skill as the
basis for extracting and refactoring a standalone `claw-radio` tool.

---

## High-level architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          dj (CLI)                               │
│  dj start / shutdown / play / queue / pause / next / station    │
└────────────────────────┬────────────────────────────────────────┘
                         │ shell scripts
          ┌──────────────┼──────────────────┐
          │              │                  │
          ▼              ▼                  ▼
     mpv engine    station controller   Telegram notifier
  (IPC socket)    (controller.py)       (notifier.py)
  /tmp/spookie-   runs on a cron /      watches mpv socket,
  mpv.sock        LaunchAgent tick,     sends "now playing"
                  feeds the queue       messages via Telegram
          │              │
          │         ┌────┴──────────┐
          │         │               │
          ▼         ▼               ▼
       mpv        yt-dlp       Chatterbox TTS
    (playback)   (resolve +   (banter audio)
                  download)
                      │
                      ▼
               SearxNG (local)
              (deep research:
               seed building)
```

---

## Source files

| File | Role |
|---|---|
| `scripts/dj` | Main CLI entry point. Routes all subcommands. |
| `scripts/dj_radio_start.sh` | Launches mpv in idle mode with IPC socket. |
| `scripts/dj_radio_shutdown.sh` | Stops mpv, unloads all LaunchAgents, removes socket. |
| `scripts/dj_radio_cmd.sh` | Low-level: sends a raw JSON command to mpv via `nc`. |
| `scripts/dj_radio_play.sh` | Resolve + play a YouTube URL/query immediately (replace). |
| `scripts/dj_radio_queue.sh` | Resolve + append a YouTube URL/query. |
| `scripts/dj_radio_pause.sh` | Pause mpv. |
| `scripts/dj_radio_resume.sh` | Unpause mpv. |
| `scripts/dj_radio_next.sh` | Skip to next item in playlist. |
| `scripts/dj_radio_stop.sh` | Stop playback but keep mpv alive. |
| `scripts/dj_radio_status.sh` | Print pause/time-pos/filename/playlist-count from mpv. |
| `scripts/dj_radio_say.sh` | Pause → macOS TTS say → afplay → unpause. Crude; superseded by Chatterbox banter. |
| `scripts/dj_station_set.sh` | Update `station_prompt` in station.json (seeds untouched). |
| `radio/controller.py` | **Core station brain.** Manages queue fill, banter, host intro, de-dupe. |
| `radio/deep_research.py` | Builds seed list by querying SearxNG, scraping Wikipedia/Discogs/MusicBrainz. |
| `radio/chatterbox_daemon.py` | Warm TTS daemon. Keeps Chatterbox model loaded; listens on Unix socket. |
| `radio/chatterbox_banter.py` | One-shot TTS fallback. Loads model each run (slow). |
| `radio/chatterbox_client.py` | Client to the warm Chatterbox daemon. |
| `radio/notifier.py` | Watches mpv IPC for `file-loaded` events; sends "now playing" to Telegram. |

---

## Data files

| File | Contents |
|---|---|
| `radio/station.json` | `station_prompt`, `seeds[]`, banter config flags. Deep research writes seeds here. |
| `radio/state.json` | Runtime state: `played_video_ids`, `played_song_keys`, `played_banter_keys`, `songs_since_banter`, `last_intro_prompt`, `seed_built`, `last_banter_ts`. |
| `radio/url_titles.json` | Map of `playback_path → human_title`. Used by notifier since raw YouTube CDN URLs have garbage titles. |
| `radio/notifier_state.json` | Stores `last_title` to suppress duplicate "now playing" messages. |
| `radio/cache/` | Downloaded audio files (rolling window, GC'd to ~8 most recent). |
| `radio/banter/` | Generated banter WAV files (GC'd to 30 most recent). |

---

## Process model

Everything runs as **macOS LaunchAgents** (plist files in `~/Library/LaunchAgents`):

| LaunchAgent | What it runs | Trigger |
|---|---|---|
| `com.pookie.dj-radio.plist` | mpv engine | `dj start` |
| `com.pookie.dj-station-controller.plist` | `controller.py` | `dj station start` — runs every ~30s |
| `com.pookie.dj-chatterbox.plist` | `chatterbox_daemon.py` | `dj station start` |
| `com.pookie.dj-radio-notify.plist` | `notifier.py` | `dj start` / `dj station start` |

The **station controller** is not a long-running daemon — it's a periodic script kicked by
launchd. Each run it checks the mpv queue depth and tops it up as needed. The
interval is ~30 seconds (configured in the plist, not in any of the scripts here).

---

## mpv IPC

All playback control goes through mpv's JSON IPC socket at `/tmp/spookie-mpv.sock`.

Commands are sent via `nc -U <socket>` with a newline-terminated JSON payload.

Key properties/commands used:

| Command / Property | Purpose |
|---|---|
| `loadfile <path> replace` | Play immediately, clearing queue |
| `loadfile <path> append-play` | Add to end; start playing if idle |
| `loadfile <path> append` | Add to end; do not auto-start |
| `playlist-move <from> <to>` | Reorder; used to insert banter "next" |
| `playlist-next force` | Skip current item |
| `playlist-clear` | Wipe queue |
| `stop` | Stop playback, keep mpv alive |
| `quit` | Kill mpv process |
| `get_property idle-active` | true when mpv is idle (nothing playing) |
| `get_property time-pos` | Current playback position (null if idle) |
| `get_property playlist-count` | Number of items in queue |
| `get_property playlist-pos` | Index of current item |
| `get_property playlist` | Full playlist array |
| `get_property path` | Current file/URL path |
| `get_property media-title` | Current track title (from metadata) |
| `enable_event file-loaded` | Subscribe to track-change events |
| `observe_property 1 media-title` | Push notifications on title change |
| `set_property pause true/false` | Pause / resume |

---

## Song sourcing pipeline (controller.py)

### 1. Seed building (deep_research.py)

Runs **once per station start** (gated by `state["seed_built"]`).

- Spins up a local **SearxNG** instance (Docker via Colima).
- Builds a set of search queries from the station prompt:
  - Infers year range (e.g. "2000s" → 2000–2009)
  - Infers region (US / UK / global)
  - Expands genre keywords
  - Generates queries for: Billboard/UK charts, Wikipedia song lists, Discogs/MusicBrainz tracklists, compilation tracklists, editorial "best of" listicles, Spotify/YouTube playlist pages
- Fetches up to 60 URLs; extracts `(artist, title)` pairs using:
  - `extract_wikitable_songs()` — parses `<table class="wikitable">` rows
  - `extract_discogs_tracklist()` — uses Discogs-specific CSS class patterns
  - `extract_musicbrainz_tracklist()` — uses MusicBrainz-specific patterns
  - `extract_generic_pairs()` — regex for "Artist - Title" patterns in stripped text
- Normalizes + dedupes up to 900 songs.
- Converts to `ytsearch10:<artist> - <title> official audio` seeds.
- Writes seeds into `station.json`.
- Sends Telegram messages to Pookie summarizing queries used and source breakdown.
- Shuts down SearxNG after.

### 2. YouTube resolution (yt_pick + yt_resolve)

For each seed (a `ytsearch10:` query):
- `yt_pick()`: runs `yt-dlp -J ytsearch10:<query>` to get 10 candidates
- Scores each candidate with `_score_candidate()`:
  - Strongly prefers: "Provided to YouTube", VEVO, "official audio"
  - Prefers: "official" (non-video), "audio" in title
  - Penalizes: live, cover, karaoke, lyrics, remix, sped up, slowed, nightcore, instrumental, extended, 1 hour, full album, playlist
  - Penalizes: duration < 90s or > 8 min; prefers 2–7 min range
  - Penalizes: live streams, is_live flag
  - Boosts: high view count (log-ish)
  - Slight penalty for long titles
- Returns `(human_title, webpage_url, meta)` for the best candidate

### 3. Download vs. stream

Preferred path: `yt_download_best_audio()` — downloads to `radio/cache/` as a
local file (avoids start/buffer latency). Falls back to streaming via `yt_resolve()`
if download fails.

### 4. De-duplication

Two levels:
- `played_video_ids` — by YouTube video ID (strongest signal)
- `played_song_keys` — by normalized title string (catches re-uploads)

`normalize_song_key()` lowercases and strips noise words like "(official audio)",
"official video", "lyrics", "hd", etc.

Last 800 plays are remembered per run in `state.json`.

---

## Banter system (controller.py)

Banter = short host voice clips inserted **between songs** in the mpv playlist.

### Rate limiting

Before generating a banter clip, checks:
- `songs_since_banter >= banter_min_gap_songs` (default: 3)
- At least 20 seconds since last banter (`last_banter_ts`)
- Random probability gate: `banter_probability` (default: 0.25 = 25% chance)
- No banter already queued in the next 4 playlist items

### Content

Two styles configured via `station.json → banter_style`:
- `"gta"` (default): 8 pre-written lines with dry GTA-style humor (traffic updates, fake sponsors, absurdist observations, station callouts)
- `"plain"`: 2 simple lines ("Stay right there — next track's a beauty")

Played banter keys are tracked to avoid repetition (up to 400 keys remembered).

### Insertion

Uses `mpv_insert_next()`:
1. `loadfile <path> append` — adds to end
2. `playlist-move <last> <pos+1>` — moves it to play immediately after current item

### Host intro

At station start (or prompt change), introduces the station once via
`maybe_enqueue_intro()`. Picks a host name from `HOSTS_BY_GENRE` based on
detected genre. Three intro line templates (randomized).

---

## TTS system

Three-layer fallback chain in `make_banter_audio()`:

1. **Warm Chatterbox daemon** (preferred, low latency)
   - `chatterbox_daemon.py` keeps model in memory
   - Unix socket at `/tmp/spookie-chatterbox.sock`
   - Client: `chatterbox_client.py`
   - Post-processes with ffmpeg `loudnorm` (target: -16 LUFS, -1.5 dBTP, LRA=11)

2. **One-shot Chatterbox** (fallback, higher latency — loads model fresh each call)
   - `chatterbox_banter.py`
   - Same loudnorm post-processing

3. **macOS TTS** (last resort)
   - `say -v Samantha -o <file.aiff> <text>`
   - No loudnorm

Model: **Chatterbox Turbo** (`ChatterboxTurboTTS`) from the `chatterbox` package.
Device: MPS (Apple Silicon GPU) with CPU fallback.
Venv: `.venv-chatterbox/` inside the dj-audio skill directory.
Output: 24 kHz WAV.

---

## Telegram notifier (notifier.py)

Runs as a persistent daemon (LaunchAgent).
- Connects to mpv IPC socket (with retry loop).
- Subscribes to `file-loaded` events and observes `media-title`.
- On each track change:
  - Looks up `url_titles.json` for the human title (CDN URLs have garbage metadata titles).
  - Falls back to `media-title` from mpv.
  - Splits "Artist - Title" format.
  - Sends Telegram message: `now playing <artist> <song>`.
- Deduplicates: won't re-send if title didn't change.
- Bot token read from `/Users/spookie/.openclaw/openclaw.json` → `channels.telegram.botToken`.

---

## Host / personality system

Hosts are statically defined in `controller.py → HOSTS_BY_GENRE`:

| Genre | Hosts |
|---|---|
| pop | Mia Parker, Tara Chase, Nina Moreno, Jules Navarro |
| country | Beau Wilder, Cassie Lynn, Hank Dalton, Shelby Rae |
| electronic | Lena Vogel, Max Adler, Anika Kraus, Jonas Richter |
| default | Ray DeMarco, Jules Navarro, Tara Chase, Danny Rivera, Nina Moreno, Mia Parker |

Host is picked deterministically per `(genre, station_prompt)` combination
so the same station always gets the same host. Genre detection is keyword-based
(`detect_genre()`).

**Important gap**: The Chatterbox TTS currently uses a single default voice for all
hosts. There is no per-host or per-genre voice differentiation yet. The design
intent (from MEMORY) is for voice to match station theme — hillbilly for country,
young California woman for pop, German-accented for electronic — but this is not
implemented yet.

---

## station.json schema

```json
{
  "station_prompt": "2000s bubblegum pop",
  "seeds": ["ytsearch10:Artist - Title official audio", "..."],
  "queue_depth": 5,
  "banter_enabled": true,
  "banter_min_gap_songs": 3,
  "banter_probability": 0.25,
  "banter_style": "gta"
}
```

Seeds are written by deep_research.py. The agent writes `station_prompt` via
`dj_station_set.sh`. All banter config has defaults in controller.py and only
needs to be in station.json to override.

---

## External dependencies

| Tool | How installed | Purpose |
|---|---|---|
| `mpv` | `brew install mpv` | Playback engine, IPC socket |
| `yt-dlp` | `brew install yt-dlp` | YouTube audio resolution + download |
| `ffmpeg` | `brew install ffmpeg` | Loudnorm post-processing on banter |
| `nc` (netcat) | built-in macOS | Send commands to mpv socket |
| Chatterbox Turbo | pip in `.venv-chatterbox` | TTS model (Apple MPS) |
| torch / torchaudio | pip in `.venv-chatterbox` | ML runtime for Chatterbox |
| SearxNG | Docker (Colima) | Private search for seed building |
| Telegram Bot API | HTTP | "Now playing" notifications |

---

## Hardcoded values / issues to fix in claw-radio

| Value | Location | Problem |
|---|---|---|
| `/Users/spookie/...` paths | Everywhere | Should be relative or config-driven |
| `/tmp/spookie-mpv.sock` | controller.py, notifier.py, scripts | Hardcoded socket path |
| `/tmp/spookie-chatterbox.sock` | daemon/client | Hardcoded socket path |
| `CHAT_ID = "1977937165"` | deep_research.py, notifier.py | Hardcoded Telegram user ID |
| `/opt/homebrew/bin/yt-dlp` | controller.py | Hardcoded binary path |
| `/opt/homebrew/bin/ffmpeg` | chatterbox_daemon.py, chatterbox_banter.py | Hardcoded binary path |
| `OPENCLAW_CFG` path | deep_research.py, notifier.py | Hardcoded to openclaw install |
| `UP`/`DOWN` SearxNG scripts | deep_research.py | Hardcoded to openclaw workspace |
| Single voice for all TTS | chatterbox_daemon/banter.py | No per-host voice cloning/selection |
| Static banter lines | controller.py | Small fixed pool; could be LLM-generated |
| Static genre detection | controller.py | Keyword-based, brittle |

---

## Known quirks / design decisions

- **station controller runs on a tick, not an event loop**: controller.py is
  stateless between runs. It loads state from state.json each time, does its
  work, and saves. This means a ~30s tick lag before the queue refills.

- **Download-first strategy**: songs are downloaded to a local cache before
  being queued in mpv. This trades disk I/O for gapless playback. The rolling
  cache keeps only the last ~8 files to avoid filling disk.

- **Banter is an audio file in the playlist**: banter clips are real WAV files
  inserted into the mpv playlist, not a separate mixer layer. This means banter
  and music are fully serialized — no overlay/ducking.

- **SearxNG for seed research**: seeds come from a private local search instance,
  not a direct YouTube search. This produces richer, more diverse results (charts,
  Wikipedia, Discogs) but requires Colima/Docker infrastructure.

- **Seeds are static after station start**: once deep_research.py runs, the
  seed list is fixed for that station session. Controller.py picks randomly from
  seeds; it does not do real-time discovery.

- **url_titles.json bridges the URL→title gap**: YouTube CDN URLs (googlevideo.com)
  carry no useful metadata. The mapping file is written when a song is queued and
  read by the notifier.

- **No mixing, no ducking, no crossfade**: songs play back to back. The "radio"
  feel comes entirely from the banter clips between songs, not audio processing.

- **No station switching**: the system supports one active station at a time.
  Switching requires `dj station stop` + `dj station set` + `dj station start`.

---

## What claw-radio should carry over vs. improve

### Carry over (solid foundations)
- mpv IPC pattern (socket-based control is clean and works well)
- `_score_candidate()` scoring logic for YouTube results
- Download-first cache strategy with rolling GC
- `played_video_ids` + `played_song_keys` dual de-dupe
- Banter insertion via `mpv_insert_next()` (playlist manipulation approach)
- Two-level TTS chain (warm daemon → one-shot → fallback)
- Loudnorm post-processing on banter for volume consistency
- `url_titles.json` URL→human title mapping pattern
- SearxNG-based deep research seed building

### Improve / redesign
- **Remove all hardcoded paths** — everything should go through a config file
- **Per-genre / per-host voice** — voice cloning or voice profile selection so
  country host sounds different from electronic host
- **LLM-generated banter** — instead of a fixed pool of 8 lines, generate banter
  dynamically using Claude (with station prompt + host persona as context)
- **Event-driven controller** — instead of a periodic tick, watch mpv events
  directly so queue refill is immediate
- **Station switching** — support multiple named stations; switch cleanly
- **CLI as a real installable tool** — `claw-radio` as a proper Python CLI
  (Click or Typer), not a bash script collection
- **Config file** — single TOML/JSON config for socket paths, binary locations,
  Telegram credentials, default volume, etc.
- **Notification abstraction** — notifier should not assume Telegram; could be
  stdout, desktop notification, or Telegram depending on config
