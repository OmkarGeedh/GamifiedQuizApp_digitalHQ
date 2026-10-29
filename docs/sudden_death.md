# Sudden Death HTTP API contract

Sudden Death is a backend-authoritative, two-option quiz mode played as HTTP turns.
The backend owns question selection and shuffling, grading, timing, progression,
scoring, and rewards. It runs over HTTP because Zoho Catalyst does not carry
WebSocket traffic; see [catalyst_websocket_issue.md](catalyst_websocket_issue.md).
Implementation: `internal/ws/turns.go` (HTTP) sharing `scoreAnswer` / `finalizeRun`
with the legacy socket game.

## Session creation

`POST /api/v1/quiz/sessions/create` with `game_mode: "sudden_death"`, `topic`
(subject code, e.g. `accounts`) and optional `chapter_no`. Questions come from
`sudden_death_questions` (exactly two options each). Explicit `question_codes` are
rejected if any is missing or not a Sudden Death question. The response omits
`correct_option`; the shuffled correct mapping stays on the server.

## Endpoints

All are `POST`, authenticated (`Authorization: Bearer`), available at `/quiz/...`
and `/api/v1/quiz/...`, and return `data.events`: the ordered game events.

| Endpoint | Body | Events returned |
|---|---|---|
| `/quiz/sudden-death/question` | `{session}` | `question` (starts its clock on first call; idempotent resync) — or `answer_result` + `game_over` if the deadline already passed |
| `/quiz/sudden-death/answer` | `{session, question, option, selected_text?, time_taken_ms}` | `answer_result`, then `question` (next) or `game_over` |
| `/quiz/sudden-death/power-up` | `{session, question, power_up: "add_time"}` | `power_up_result` |

Gameplay rejections are returned as an `error` event with HTTP 200 (see error codes).
HTTP errors are reserved for transport problems: `400` bad body / not a Sudden Death
session, `401` auth, `403` another player's session, `404` unknown session, `500`
persistence/finalization failure.

```json
{
  "success": true,
  "data": {
    "events": [
      { "type": "answer_result", "data": { "question": "acc11-01-sd-0003", "is_correct": true, "...": "..." } },
      { "type": "question", "data": { "question": "acc11-01-sd-0007", "remaining_time_ms": 15000, "...": "..." } }
    ]
  }
}
```

## Timing

| Rule | Value |
|---|---|
| Question time | 15 s, server authoritative (`question_deadline` on the cached session) |
| Clock start | first `/question` call for that question; the next question's deadline also includes the 400 ms result transition |
| HTTP grace | an answer received up to 1 s after the deadline is still graded (network jitter); later is a timeout |
| Timeout | client posts `option: "timeout"` when `remaining_time_ms` runs out; any later request also resolves it |

There is no disconnect pause: a backgrounded app keeps the clock running. Sessions
expire after 5 minutes of inactivity (`SessionInactivityTTL`).

## Question event

```json
{
  "type": "question",
  "data": {
    "question": "acc11-01-sd-0003",
    "prompt": "Which scholar defined book-keeping as 'an art of recording business dealings in a set of books'?",
    "options": [
      { "option": "a", "text": "J. R. Batliboi" },
      { "option": "b", "text": "Luca Pacioli" }
    ],
    "points": 3,
    "time_limit_ms": 15000,
    "remaining_time_ms": 15000,
    "question_number": 1,
    "total_questions": 10,
    "server_timestamp_ms": 1793286822422
  }
}
```

Option order is shuffled per session. Clients submit the displayed option letter.
`selected_text` cannot override a contradictory option letter.

## Skip and Add 5 seconds

`option: "skip"` awards zero points, resets the streak, increments
`skipped_count`, and advances. It does not end the run.

`add_time` may be used once per run, before the deadline. It extends the
authoritative deadline by exactly 5000 ms:

```json
{ "type": "power_up_result", "data": { "question": "acc11-01-sd-0003", "power_up": "add_time", "added_time_ms": 5000, "remaining_time_ms": 17200 } }
```

`fifty_fifty` is rejected with `unsupported_power_up` (the mode has two options).
Wallet debits for power-ups are unchanged by this protocol.

## Progression and completion

| Outcome | Result |
|---|---|
| Correct | Award points and continue |
| Skip | Zero points, reset streak, continue |
| Wrong | Zero points and end with `wrong_answer` |
| Timeout | Zero points and end with `timeout` |
| Reach the end | End with `completed` |
| Inactivity | Session expires and is treated as `abandoned` |

Only reaching the end without a wrong answer or timeout sets
`completed_successfully: true`. A run with skips can still complete. Each answer
takes a per-question lock, so a double tap cannot be graded twice.

## Scoring and rewards

- Correct answer: 10 points.
- Every third consecutive correct answer: additional 5 points.
- Skip, wrong answer, timeout: 0 points (the latter two end the run).
- No speed bonus.

XP and coin formulas: [points_calculation.md](points_calculation.md).

## Game-over event

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

`level_up_reward` is a separate economy event and is omitted when no level-up occurs.
Rewards are committed atomically with the session (`repo.FinalizeSession`) before
`game_over` is returned.

## Error codes (`error` event)

`missing_question`, `missing_option`, `stale_answer` (wrong/old question, question
not served yet, or duplicate submit), `unsupported_power_up`,
`power_up_already_used`, `session_not_active`. An error never advances the question.
