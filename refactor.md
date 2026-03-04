# Agent-First Radio Refactor Plan

This document maps out a refactor to simplify the CLI and make autonomous radio playback work cleanly with agent-generated banter.

## 1. Problem Summary

Current behavior creates workflow friction:

- Event streaming (`events --json`) is awkward for agents because it blocks indefinitely.
- Banter timing is late (`track_started` happens after playback begins).
- Playback/control semantics are confusing (`seed` works while stopped, `next` can feel like "start playback").
- Command surface mixes human and agent workflows.

## 2. Refactor Goals

- Keep radio playback autonomous once started.
- Give the agent deterministic, blocking, one-event-at-a-time interaction.
- Emit banter tasks before the next song starts.
- Make `say` fulfill explicit banter tasks and inject audio in the correct place.
- Minimize command complexity for v1.

## 3. Proposed Agent Workflow

1. `claw-radio search ...` (repeat as needed)
2. `claw-radio seed '[...]' [--append]`
3. `claw-radio say "<intro banter>"` (optional intro before music)
4. `claw-radio start` (starts autonomous playback)
5. Loop:
   - `claw-radio poll --json --timeout 30`
   - react to returned event (`banter_needed`, `queue_low`, `engine_stopped`, `timeout`)

Radio continues autonomously between agent actions.

## 4. Command Contract Changes

### Keep

- `search`
- `seed` / `seed --append`
- `say`
- `start`
- `stop`
- `status`
- `next`, `pause`, `resume`, `queue` (advanced/manual overrides)

### Add

- `poll --json [--timeout <seconds>]`
  - blocks until exactly one actionable event or timeout
  - prints one JSON object and exits

### De-emphasize

- `events --json` remains optional/debug stream, not the primary agent control loop

## 5. Event Model (Poll)

All events should include:

- `event`: string
- `event_id`: stable unique id for idempotency
- `ts`: unix timestamp

### 5.1 banter_needed (new core event)

Emitted before next track playback.

Payload:

- `prompt`: canonical prompt text for LLM (eg. "Generate banter for the next song")
- `next_song`: `{ "artist": "...", "title": "..." }`
- `deadline_ms`: suggested response deadline
- `voice`: optional voice hint

Example:

```json
{
  "event": "banter_needed",
  "event_id": "evt_01J...",
  "ts": 1772512345,
  "prompt": "Generate banter for the next song in 1-2 short sentences.",
  "next_song": {"artist": "Kendrick Lamar", "title": "Money Trees"},
  "deadline_ms": 3000,
  "voice": "hip-hop"
}
```

### 5.2 queue_low

Payload:

- `count`
- `depth`
- optional `suggested_action`: "search_and_seed_append"

### 5.3 engine_stopped

Signals crash/stop; agent should call `start`.

### 5.4 timeout

Returned by `poll` when no actionable event occurred within timeout.

## 6. say Command Semantics

Add explicit fulfillment mode:

- `claw-radio say "..." --for <event_id>`

Behavior:

- Generate TTS clip.
- Attach clip to pending `banter_needed` event.
- Ensure clip is injected before the associated next song.
- Idempotent: duplicate `--for` on same event_id is safe/no-op.

Keep plain `say "..."` for intro/manual interjections.

## 7. Controller Playback Timeline

For each upcoming song:

1. Resolve next track.
2. Emit `banter_needed` event with `event_id` and song context.
3. Wait up to `deadline_ms` for `say --for event_id`.
4. If banter received, queue banter clip before song.
5. If not received by deadline, continue without banter.
6. Start song playback.

Important: playback never deadlocks waiting for agent input.

## 8. State and Idempotency

Introduce lightweight pending-event state (in controller runtime state):

- `pending_banter_event_id`
- `pending_song`
- `deadline_at`
- `fulfilled` bool

Rules:

- Only one active `banter_needed` at a time.
- `say --for` must match active event_id.
- Fulfilled events cannot be fulfilled again.

## 9. Backward Compatibility Strategy

Phase-safe approach:

1. Add `poll` and `banter_needed` while keeping `events`.
2. Add `say --for` while keeping existing `say` behavior.
3. Update SKILL/docs to use `poll` loop as primary.
4. Optionally deprecate stream loop usage later.

## 10. Acceptance Criteria

- Agent can run end-to-end with commands: `search -> seed -> say -> start -> poll/react`.
- `poll` never blocks forever when timeout set.
- Banter clip plays before target song when provided in time.
- Playback continues autonomously if banter is late/missing.
- No manual `next` required to begin playback after `start` with seeded tracks.
- `queue_low` reliably prompts append behavior.

## 11. Test Plan (High Level)

- Unit tests:
  - event queueing and timeout behavior for `poll`
  - `say --for` idempotency and event matching
  - pre-song injection ordering
- Integration tests:
  - controller emits `banter_needed` before each track
  - no deadlock when agent does not respond
  - `queue_low` emitted at threshold
- CLI tests:
  - `poll --json --timeout` returns `timeout` event
  - `say --for` invalid event id returns clear error

## 12. Open Questions

- Should `poll` return any non-actionable telemetry events, or only actionable ones?
- Default `deadline_ms` value (eg. 2500 vs 4000)?
- Should intro banter always be manual `say` before `start`, or auto-event (`intro_needed`) on first start?

## 13. Recommendation

Proceed with this refactor in two increments:

1. Introduce `poll` + `banter_needed` + `say --for` contract.
2. Clean up start/seed/playback semantics and docs to make this the default agent path.
