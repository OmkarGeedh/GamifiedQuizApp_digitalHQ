# Sudden Death WebSocket API contract

Sudden Death is a backend-authoritative, two-option quiz mode served over
`GET /ws/game` (also available at `GET /api/v1/ws/game`). The backend owns
question selection and shuffling, grading, timing, progression, scoring, and
rewards.

## Session creation and question contract

Create the session with `POST /api/v1/quiz/sessions/create` and
`game_mode: "sudden_death"` before opening the socket.

Every selected question must:

- have `question_type: "sudden_death"`;
- contain exactly two non-empty options; and
- identify one of those options as correct.

Explicit `question_codes` are rejected if any requested question is missing or
belongs to another mode. WebSocket recovery also reloads questions by topic and
mode, so MCQ questions cannot enter a Sudden Death session. The session creation
response omits `correct_option`; the shuffled correct mapping remains on the
server.

## Connection and resync

| Property | Value |
| --- | --- |
| Endpoint | `GET /ws/game` or `GET /api/v1/ws/game` |
| Auth | `clientAccessToken` cookie, bearer header, or `?token=` query |
| Required query | `session_id` |
| Question timer | 15 seconds, server authoritative |

The first server frame is the active `question`. A reconnect or an idempotent
`join_game` request returns that question with the current
`remaining_time_ms`.

```json
{
  "type": "join_game",
  "data": { "session": "<session-uuid>" }
}
```

The in-process session retains the authoritative deadline, including an
accepted `add_time` extension. During a disconnect the remaining budget is
paused and resumes when the player reconnects. Deadline state is not currently
durable across a backend process restart.

## Question frame

```json
{
  "type": "question",
  "data": {
    "question": "sd_acc_001",
    "prompt": "Which account is debited when goods are purchased for cash?",
    "options": [
      { "option": "a", "text": "Purchases Account" },
      { "option": "b", "text": "Cash Account" }
    ],
    "points": 10,
    "time_limit_ms": 15000,
    "remaining_time_ms": 15000,
    "question_number": 1,
    "total_questions": 10
  }
}
```

Option order is shuffled per session. Clients must submit the displayed option
letter. The original database letter cannot make a shuffled answer correct, and
`selected_text` cannot override a contradictory option letter.

## Submit answer and Skip

```json
{
  "type": "submit_answer",
  "data": {
    "question": "sd_acc_001",
    "option": "a",
    "selected_text": "optional compatibility value",
    "time_taken_ms": 2500
  }
}
```

Use `"option": "skip"` for Skip. Skip awards zero points, resets the current
streak, increments `skipped_count`, and advances to the next question. It does
not end the run.

The backend checks receive time against the authoritative deadline before
accepting an answer or Skip. At or after the deadline, timeout wins.

An `answer_result` reports backend-authoritative correctness, the shuffled
`correct_option`, points earned, total score, skip status, and timeout status.

## Add 5 seconds

Sudden Death supports one `add_time` use per in-process session:

```json
{
  "type": "use_power_up",
  "data": {
    "session": "<session-uuid>",
    "question": "sd_acc_001",
    "power_up": "add_time"
  }
}
```

If accepted before the deadline, the backend extends the actual question
deadline and timer by exactly 5000 milliseconds and replies:

```json
{
  "type": "power_up_result",
  "data": {
    "question": "sd_acc_001",
    "power_up": "add_time",
    "added_time_ms": 5000,
    "remaining_time_ms": 9200
  }
}
```

Stale, late, duplicate, inactive-session, and post-answer requests return an
`error` frame without closing an otherwise healthy connection. Wallet debit
ownership is unchanged by this protocol; the backend does not introduce a new
automatic debit here.

`fifty_fifty` is rejected with `unsupported_power_up` in Sudden Death because
the mode already has only two options. Existing MCQ 50:50 behavior is unchanged.

## Progression and completion

| Outcome | Result |
| --- | --- |
| Correct | Award points and continue |
| Skip | Zero points, reset streak, continue |
| Wrong | Zero points and end with `wrong_answer` |
| Timeout | Zero points and end with `timeout` |
| Reach the end | End with `completed` |
| Inactivity/abandonment | Persist `abandoned` |

Only reaching the end without a wrong answer or timeout sets
`completed_successfully: true`. A run containing skips can still complete.
Terminal handling is idempotent, so competing late events cannot produce a
second `game_over`.

## Scoring and rewards

- Correct answer: 10 points.
- Every third consecutive correct answer: additional 5 points.
- Skip: 0 points and resets the current streak.
- Wrong answer or timeout: 0 points and ends the run.
- No speed bonus.

The separate XP and coin formulas are documented in
[points_calculation.md](points_calculation.md).

## Game-over frame

```json
{
  "type": "game_over",
  "data": {
    "final_score": 115,
    "total_questions": 10,
    "correct_count": 10,
    "skipped_count": 0,
    "best_streak": 10,
    "coins_earned": 44,
    "xp_earned": 90,
    "gems_earned": 3,
    "new_level": 2,
    "did_level_up": true,
    "level_up_reward": { "coins": 50, "xp": 0, "gems": 1 },
    "game_mode": "sudden_death",
    "end_reason": "completed",
    "ended_early": false,
    "completed_successfully": true
  }
}
```

`level_up_reward` is a separate economy event and is omitted when no level-up
occurs. `game_over` is emitted exactly once, followed by a normal WebSocket
close.

## Error codes

Common codes are `invalid_payload`, `missing_question`, `stale_answer`,
`missing_option`, `unsupported_power_up`, `power_up_already_used`,
`session_mismatch`, `session_not_active`, `transition_in_progress`, and
`internal_error`.

An error frame does not advance the question. Persistence or finalization
failures are logged; a failed authoritative finalization returns
`internal_error` and closes the connection instead of reporting a successful
game over.
