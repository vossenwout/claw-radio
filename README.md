# claw-radio

`claw-radio` is a CLI for running an AI-operated, GTA-style radio station:
start continuous playback, build a seed list from web search, inject spoken
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
claw-radio seed '["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]'
claw-radio poll --json
```

## CLI Reference

| Command | Flags | Description |
| --- | --- | --- |
| `tts install` | `none` | Install Chatterbox TTS files into the configured data directory. |
| `tts voice add` | `--name <voice-name>` | Download a sample URL and save it as a reusable voice prompt WAV. |
| `start` | `none` | Start the mpv engine and controller daemon. |
| `stop` | `none` | Stop the mpv engine and controller daemon and remove PID files. |
| `next` | `none` | Skip to the next track. |
| `seed` | `--append` | Replace or append station seeds from a JSON string array of `"Artist - Title"` items. |
| `search` | `--mode raw\|artist-top\|artist-year\|chart-year\|genre-top, --engines <csv>, --max-pages <n>, --expand-suggestions, --debug` | Query SearxNG and print extracted `Artist - Title` candidates as JSON. |
| `say` | `--voice <name-or-path>, --for <event-id>` | Render TTS banter. With `--for`, fulfill a pending `banter_needed` event; without it, queue immediate or next-start intro banter. |
| `poll` | `--json, --timeout <duration>` | Block until one actionable controller event, then print and exit. |
| `status` | `--json` | Print a runtime state snapshot in text or JSON. |
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

## Seed Format

- `seed` expects one JSON array argument: `['Artist - Title', ...]`.
- Keep entries as plain strings in `Artist - Title` format.

```bash
claw-radio seed '[
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
