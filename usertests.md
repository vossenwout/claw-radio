# User Workflow Test Matrix

Goal: simulate real host behavior for a long session and validate CLI/agent UX end to end.

Legend:
- `PASS`: behavior matches expected UX
- `FAIL`: bug found; must be fixed and re-tested
- `SKIP`: not run

## Scenario Checklist

| ID | Scenario | Expected | Status | Notes |
|---|---|---|---|---|
| S01 | `poll` while stopped | Immediate `engine_stopped` cue | PASS | Verified JSON cue with restart instruction. |
| S02 | `start` idempotent | "already running" when already running | PASS | Repeated `start` returned no-op message. |
| S03 | `stop` idempotent + reset | first call: stopped+reset, second call: already stopped | PASS | First call reset runtime/state/cache; second call no-op. |
| S04 | `stop` clears playlist/state/cache | playlist empty + cache gone after stop | PASS | Confirmed empty playlist and removed state/cache dirs. |
| S05 | `playlist add` dedupe | duplicate add does not duplicate queue item | PASS | 2nd add returned `Added 0 songs`. |
| S06 | Intro before start | `say` while stopped queues intro | PASS | `queued intro banter for next start` shown. |
| S07 | Start with intro + one song | startup prepares first song; playback starts reliably | PASS | Start blocks with prepare message, then playback starts. |
| S08 | `status` UX | shows now playing + `upcoming songs` (no internals) | PASS | Human/JSON output clear; no count/depth jargon. |
| S09 | Queue low loop | `queue_low` cue appears when upcoming is low | PASS | Observed cue and responded by adding songs. |
| S10 | Add songs mid-playback | songs get picked up and queued while current track keeps playing | PASS | Added songs during playback, queue/upcoming increased later. |
| S11 | Banter cue after mid-play add | `banter_needed` appears for newly upcoming song | PASS | Observed cue after song finished buffering/queueing. |
| S12 | `buffering` cue behavior | timeout while preparing songs maps to `buffering` cue | PASS | Idle-running + pending songs returned `buffering` cue. |
| S13 | Banter injection while running | `say` queues host line to play next | PASS | `queued banter` and upcoming count increased by one. |
| S14 | Playlist consumption semantics | played songs disappear from `playlist view` | PASS | Current song removed from playlist as playback progressed. |
| S15 | Next track continuity | after song ends, next queued song starts automatically | PASS | Verified ABBA -> banter -> a-ha -> banter -> Whitney. |
| S16 | Long host loop stability | multiple poll/add/say cycles stay coherent | PASS | Ran multi-cycle host session with repeated add/poll/say/status. |

## Host Roleplay Session Script (high level)

1. Cold start cleanup (`stop`, verify reset behavior).
2. Queue intro + first track, start show, poll/react.
3. As host, react to cues:
   - `queue_low` -> add 2-4 tracks
   - `banter_needed` -> `say` short host line
   - `buffering`/`timeout` -> wait + poll again
4. Repeat cycles until at least two full song transitions occur.
5. Validate that queue, playlist view, and status remain consistent.

## Defect Log

| Bug ID | Found In | Symptom | Root Cause | Fix | Re-test |
|---|---|---|---|---|---|
| B01 | stop/reset path | `stop` intermittently failed with `directory not empty` | removeAll raced with process shutdown/file writes | Added retry/backoff for recursive delete in stop cleanup | PASS |
| B02 | poll cues | `banter_needed` emitted for banter audio filename | upcoming-track filter did not exclude banter paths | Skip banter cue generation when next track is banter audio | PASS |
| B03 | live queue updates | songs added during playback sometimes not picked up | controller held stale in-memory playlist state | Reload station state from disk during fill + consume updates atomically | PASS |
| B04 | responsiveness | playlist consumption/cues delayed during startup fills | fillQueue could resolve multiple songs in one blocking pass | Limit to one resolve per fill cycle to reduce event-loop starvation | PASS |
