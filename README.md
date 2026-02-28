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
claw-radio seed '["Britney Spears - Oops! I Did It Again", "NSYNC - Bye Bye Bye"]' --label "2000s pop"
claw-radio events --json
```

## CLI Reference

| Command | Flags | Description |
| --- | --- | --- |
| `tts install` | `none` | Install Chatterbox TTS files into the configured data directory. |
| `tts voice add` | `--name <voice-name>` | Download a sample URL and save it as a reusable voice prompt WAV. |
| `start` | `none` | Start the mpv engine and controller daemon. |
| `stop` | `none` | Stop the mpv engine and controller daemon and remove PID files. |
| `play` | `none` | Queue a query or URL next and immediately skip to it. |
| `queue` | `none` | Append a query or URL to the end of the current playlist. |
| `pause` | `none` | Pause playback in mpv. |
| `resume` | `none` | Resume playback in mpv. |
| `next` | `none` | Skip to the next track. |
| `seed` | `--label <text>, --append` | Replace or append station seeds from a JSON string array. |
| `search` | `none` | Query SearxNG and print extracted `Artist - Title` candidates as JSON. |
| `say` | `--voice <name-or-path>` | Render TTS banter and insert it after the current track. |
| `events` | `--json` | Stream playback/controller events as human-readable text or NDJSON. |
| `status` | `--json` | Print a runtime state snapshot in text or JSON. |
| `version` | `none` | Print the installed `claw-radio` version. |
