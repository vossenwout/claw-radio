# claw-radio

`claw-radio` is a CLI for running an AI-operated, GTA-style radio station:
start continuous playback, build a playlist pool from web search, inject spoken
banter, and react to playback events.

## Installation

### macOS (Homebrew)

```bash
brew install vossenwout/tap/claw-radio-cli
```

### Linux (curl + tar)

```bash
curl -fsSL -o claw-radio.tar.gz https://github.com/vossenwout/claw-radio/releases/latest/download/claw-radio_Linux_x86_64.tar.gz
tar -xzf claw-radio.tar.gz
sudo install -m 0755 claw-radio /usr/local/bin/claw-radio
```

## Dependencies

Install runtime media tools before running `claw-radio start`:

### macOS

```bash
brew install mpv yt-dlp ffmpeg
```

### Linux (Debian/Ubuntu)

```bash
sudo apt update
sudo apt install -y mpv yt-dlp ffmpeg
```

## Optional TTS Setup

```bash
claw-radio tts install
claw-radio tts voice add "https://www.youtube.com/watch?v=<sample>" --name pop
```

## Quick Start

```bash
claw-radio start
claw-radio playlist add '["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]'
claw-radio poll
```

## CLI Reference

| Command | Flags | Description |
| --- | --- | --- |
| `tts install` | `none` | Install Chatterbox TTS files into the configured data directory. |
| `tts voice add` | `--name <voice-name>` | Download a sample URL and save it as a reusable voice prompt WAV. |
| `start` | `none` | Start the radio show. If already running, nothing changes. |
| `stop` | `none` | End the current radio session. If already stopped, it returns a no-op message. |
| `reset` | `none` | Start fresh by stopping the radio and clearing playlist pool, station state, and cache. |
| `next` | `none` | Skip to the next track. |
| `playlist add` | `none` | Add songs to the playlist pool from a JSON string array of `"Artist - Title"` items. |
| `playlist view` | `--json` | Show songs currently in the playlist pool. |
| `playlist reset` | `none` | Clear all songs from the playlist pool. |
| `search` | `--mode raw\|artist-top\|artist-year\|chart-year\|genre-top, --engines <csv>, --max-pages <n>, --expand-suggestions, --debug` | Find song ideas you can add to your playlist pool. |
| `say` | `none` | Speak a host line next. If radio is not running, it becomes your intro on next start. |
| `poll` | `--timeout <duration>` | Wait for the next host cue and return one JSON result. |
| `status` | `--json` | Check whether the radio is running, what is playing, and queue health. |
| `version` | `none` | Print the installed `claw-radio` version. |

## Search Tips

- Use `search --mode raw "<query>"` for deterministic one-query behavior (advanced/debug fallback).
- Use `search --mode artist-top "<artist>"` when you want popular songs for an artist.
- Use `search --mode artist-year "<artist> <year>"` when you need precise artist + year targeting.
- Use `search --mode chart-year "Billboard Year-End Hot 100 2024"` for chart/year list queries.
- Use `search --mode genre-top "house music classics"` for broad genre discovery.
- Combine modes with commas, for example: `search --mode chart-year,genre-top "best 2000s pop songs"`.
- Use `search --engines yahoo,bing "<query>"` to override Searx engines per command.
- Set default engine filters in `config.json` under `search.engines` and `search.mode_engines`.
- Add `--debug` to inspect query expansion, page fetch outcomes, and ranking counts.

## Playlist Format

- `playlist add` expects one JSON array argument: `['Artist - Title', ...]`.
- Keep entries as plain strings in `Artist - Title` format.

```bash
claw-radio playlist add '[
  "Kendrick Lamar - Alright",
  "Kendrick Lamar - DNA.",
  "SZA - Saturn",
  "Outkast - Hey Ya!",
  "Daft Punk - One More Time",
  "The Weeknd - Blinding Lights",
  "Aaliyah - Try Again",
  "Fleetwood Mac - Dreams"
]'
```
