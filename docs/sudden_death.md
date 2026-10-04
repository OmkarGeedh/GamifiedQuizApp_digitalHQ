# Sudden Death — WebSocket API contract

Protocol reference for the real-time MCQ mode served at `GET /ws/game`.

## Authority and scope

The backend is authoritative for question order, option shuffling, grading, per-question
timing, scoring, and rewards. Clients render and submit; they never compute a result.

Scoring values live in [points_calculation.md](points_calculation.md) and are not restated
here. This document covers the live WebSocket transport only.

> **Status.** Fully implemented and covered by `internal/ws/` tests. `join_game`,
> `use_power_up`, `power_up_result`, and the `error.code` field are additive: existing
> clients that ignore them keep working. The only protocol change to a previously
> shipped field is that `question.remaining_time_ms` is now always serialised rather
> than omitted when zero.

## Connection

| Property | Value |
| --- | --- |
| Endpoint | `GET /ws/game` (primary), `GET /api/v1/ws/game` (mirror) |
| Upgrade | WebSocket, RFC 6455 |
| Auth | `clientAccessToken` cookie → `Authorization: Bearer` → `?token=` query |
| Required query | `session_id` (UUID) |
| Max inbound frame | 4096 bytes |
| Keepalive | server pings every 54s; read deadline 60s, extended on pong |

Auth precedence is fixed in `internal/middleware/auth.go`. WebSocket clients must use the
`?token=` query form, since browsers cannot attach an `Authorization` header to an upgrade
request.

A session must already exist and be `in_progress` before connecting. Create one with
`POST /api/v1/quiz/sessions/create`.

### Reconnection and concurrent sockets

If a player opens a new connection with the same `session_id` while an existing socket is attached (e.g. following a page reload or mobile network switch), the server gracefully closes the previous connection and binds the new socket to the ongoing session.

## Pre-upgrade rejections

These are ordinary HTTP responses in the standard envelope
(`success` / `message` / `timestamp`), emitted **before** the upgrade. A client must inspect
the handshake status — it will not receive an `error` frame.

| Status | Cause |
| --- | --- |
| 401 | missing, malformed, expired, or revoked token |
| 400 | `session_id` absent or blank |
| 404 | session does not exist |
| 403 | session belongs to a different player |
| 409 | session is not `in_progress`, or was abandoned after 5 minutes of inactivity |
| 500 | session questions could not be loaded |

## Envelope

Both directions use the same wrapper:

```json
{ "type": "<message_type>", "data": { } }
```

## Message types

| `type` | Direction | Status |
| --- | --- | --- |
| `join_game` | client → server | implemented, idempotent resync |
| `submit_answer` | client → server | implemented |
| `use_power_up` | client → server | implemented, 50:50 power-up |
| `question` | server → client | implemented |
| `answer_result` | server → client | implemented |
| `power_up_result` | server → client | implemented |
| `game_over` | server → client | implemented, sent exactly once |
| `error` | server → client | implemented, with machine-readable `code` |

## Client → server

### `submit_answer`

```json
{
  "type": "submit_answer",
  "data": {
    "question": "ACC001",
    "option": "b",
    "selected_text": "optional full answer text",
    "time_taken_ms": 2500
  }
}
```

- `question` — the `question` value from the `question` frame being answered. Validated
  against the question currently served; a mismatch is rejected with an `error` of code
  `stale_answer` and changes nothing.
- `option` — `a`, `b`, `c`, `d`, `skip`, or the full option text.
- `selected_text` — optional, for submitting the answer string rather than a letter.
- `time_taken_ms` — advisory. Clamped to 17000 (15s limit + 2s grace). **Never affects
  scoring**; stored for history only.

`"skip"` is evaluated as an unanswered question with no penalty, identical to REST.

### `join_game`

```json
{ "type": "join_game", "data": { "session": "<session-uuid>" } }
```

Replies with the current `question`. Idempotent, and safe to send on an already-attached
socket. The `session` field is optional; if present it must match the socket's `session_id`
(a mismatch is rejected with an `error` of code `session_mismatch`).
Useful for re-syncing after a reconnect without opening a new socket.

### `use_power_up`

```json
{
  "type": "use_power_up",
  "data": { "session": "<uuid>", "question": "ACC001", "power_up": "fifty_fifty" } 
}
```

Only `fifty_fifty` is supported. Any other value is rejected with an `error`. This mirrors
the REST endpoint `POST /api/v1/quiz/power-ups/fifty-fifty`. The `session` field is optional;
if present it must match the socket's `session_id` (mismatch rejected with `session_mismatch`).

The 50:50 power-up is a single-use lifeline per quiz session (once per entire game run). Using it a second time in the same session is rejected with an `error` of code `power_up_already_used`.

## Server → client

### `question`

```json
{
  "question": "ACC001",
  "prompt": "Which of the following is a liability?",
  "points": 10,
  "hint": "optional",
  "options": [
    { "option": "a", "text": "..." },
    { "option": "b", "text": "..." },
    { "option": "c", "text": "..." },
    { "option": "d", "text": "..." }
  ],
  "time_limit_ms": 15000,
  "remaining_time_ms": 9500,
  "question_number": 1,
  "total_questions": 3
}
```

Option order is shuffled per session. `time_limit_ms` is always 15000. `remaining_time_ms` is always present and carries the exact milliseconds left on the server timer — it equals `time_limit_ms` on a fresh question, and is lower on a reconnect or `join_game` resync. It can legitimately be `0`; treat that as expired rather than falling back to `time_limit_ms`.

### `answer_result`

```json
{
  "question": "ACC001",
  "option": "b",
  "correct_option": "b",
  "is_correct": true,
  "is_skipped": false,
  "explanation": "optional",
  "points_earned": 10,
  "coins_earned": 0,
  "your_score": 10,
  "is_timeout": false
}
```

Client notes:

- **`coins_earned` is always `0` here.** Coins are awarded only on session completion. See
  `game_over`.
- On timeout, `option` is the literal string `"timeout"` and `is_timeout` is `true`.
- On skip (`option: "skip"`), `is_skipped` is `true`, `is_correct` is `false`, and `points_earned` is `0`.
- `correct_option` is the **shuffled** letter for this session, not the original database
  letter.
- This frame has no `combo_streak` and no `total_questions`, unlike the REST
  `answer_result`. Clients should track streak and progress from `question_number` /
  `total_questions`.

### `power_up_result`

```json
{ "question": "ACC001", "hidden_options": ["c", "d"] }
```

Exactly two incorrect options to hide. The correct option is never returned.

### `game_over`

```json
{
  "final_score": 20,
  "total_questions": 3,
  "correct_count": 2,
  "coins_earned": 11,
  "xp_earned": 25,
  "gems_earned": 0,
  "new_level": 2,
  "did_level_up": true,
  "level_up_reward": { "coins": 50, "xp": 0, "gems": 1 }
}
```

- `level_up_reward` is a **separate** economy event. It is not included in `coins_earned`
  or `xp_earned`, and is omitted entirely when `did_level_up` is `false`.
- `level_up_reward.xp` is always `0`. The level-up event grants coins and gems only; the
  field exists for parity with the REST completion payload, which also leaves it unset.
- `final_score` equals `correct_count` × 10. Speed, difficulty, combo, and power-ups do not
  affect it.
- Sent exactly once. The server then sends a WebSocket Close frame (`1000 Normal Closure`) with a 1-second grace period before closing the underlying TCP connection.

### `error`

Every `error` frame carries a machine-readable `code`:

```json
{ "code": "stale_answer", "message": "stale answer: current question is ACC002" }
```

| `code` | Meaning |
| --- | --- |
| `invalid_payload` | frame could not be unmarshalled, or a payload field is malformed |
| `unsupported_type` | `type` is not a recognised inbound message type |
| `missing_question` | `submit_answer` or `use_power_up` sent without a `question` |
| `stale_answer` | `question` does not match the question currently served |
| `missing_option` | `submit_answer` sent without an `option` |
| `unsupported_power_up` | `power_up` is not `fifty_fifty` |
| `power_up_already_used` | `fifty_fifty` power-up has already been used in this session |
| `session_mismatch` | `session` provided in payload does not match the socket's `session_id` |
| `session_not_active` | session has finished or been abandoned |
| `transition_in_progress` | `join_game` arrived during the 800ms transition; retry shortly |
| `internal_error` | persistence or grading failure |

An `error` frame is informational. It never advances the question and never ends the game —
the server keeps waiting until the timer expires.

## Lifecycle

```
connect ──> pre-upgrade checks ──> upgrade
                                    │
                            attach player, start timer
                                    │
                                    v
                              question  (time_limit_ms)
                                    │
                ┌───────────────────┴───────────────────┐
                │                                       │
        submit_answer                             timer expiry
                │                                       │
                └───────────────────┬───────────────────┘
                                    v
                             answer_result
                                    │
                      more questions? ──no──> game_over ──> close
                           │yes
                       800ms pause
                           │
                           v
                     question (next)
```

Guarantees:

1. Exactly one `game_over` per session, after which the server sends a Close frame (code 1000) and closes the socket after a 1s grace period.
2. Each `question` is either the first frame, or immediately preceded by an
   `answer_result` for the previous question.
3. The server does not advance while no player is attached: the 15-second question
   timer pauses on disconnect and resumes with the remaining time upon reconnection (communicated via `remaining_time_ms`).
4. If a player disconnects during the 800ms transition delay, the server holds the next question in a paused state, delivering it with the full 15s timer once the player reconnects.
5. `final_score`, `xp`, and `coins` come from the backend; clients only display them.
6. The 15-second question timer begins only after the 800ms inter-question transition
   completes and the `question` frame is delivered (giving players the full 15s).
7. Answers or power-ups received during the 800ms transition interval are rejected as
   `stale_answer`.
8. Only the connection that currently owns the session may mutate it. When a reconnect
   supersedes an existing socket, the old socket's disconnect is a no-op: the live player
   keeps its question timer and its remaining budget is not decremented a second time.

## Session expiry

A session with no activity for 5 minutes is marked `abandoned` by a background sweeper and
awards nothing. The sweeper runs every 60s server-side, so client-visible abandonment can lag
the real deadline by up to a minute. Connecting to an expired session returns HTTP 409.
The in-memory session goroutine auto-terminates and cleans itself up from the session manager
after 5 minutes of continuous disconnection, matching the background sweeper.

## Related

- [Points, scoring and rewards](points_calculation.md) — the scoring authority
- [Authentication](auth.md) — JWT lifecycle, refresh, and OTP *(currently empty)*