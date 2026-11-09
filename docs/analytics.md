# Analytics

A small log of what the app reports that the server cannot see for itself, plus ready-made reports. No outside service, no personal data: everything stays in your own database.

## Where the numbers come from

| Question | Source | Needs the new table? |
|---|---|---|
| Do players come back (day 1, 7, 30)? Players per day? | `client_activity_days` | No |
| Games finished, per mode | `leaderboard_game_results` | No |
| Are daily goals, streak rewards, freezes, spins used? | `wallet_ledger` | No |
| Reminder choices? Where do players quit a game? | `analytics_events` (migration `000008_analytics_events`) | Yes |

The ready-made queries are in the migration repo: `analytics/reports.sql`. They are read-only; run them in any SQL console.

Note: an "active day" means a day the player opened the app (the profile load records it), the same thing the streak counts.

## `POST /analytics/events`

Signed-in players only. At the root and under `/api/v1`. Rate limited to 30 per minute per player.

```json
{ "events": [
    { "name": "reminder_enabled" },
    { "name": "game_quit", "props": { "mode": "mcq", "question_index": 4 }, "at": "2026-03-10T11:58:00Z" }
] }
```

Always answers `202` with `{ "accepted": n, "dropped": m }`. Analytics must never get in the way of the app, so:

- An event with an unknown name or unusable details is **dropped on its own**; the rest of the batch is kept.
- If storing fails (for example the migration has not been applied yet), it is logged (at most once a minute) and the player still gets `202`. Nothing is shown to the player.
- Only an empty batch, more than 25 events, or a body over 64 KB is refused (`400`).

### What is allowed

Event names (`dto.AnalyticsEventNames`): `app_open`, `reminder_prompt_shown`, `reminder_enabled`, `reminder_declined`, `reminder_blocked`, `game_quit`.

`props` is optional: a flat object of at most 8 keys, under 512 bytes in total.
- Keys: lowercase snake_case, up to 24 characters.
- Values: a number, a boolean, or a short identifier (`a-z`, `0-9`, `_`, `-`, up to 64 characters, no spaces or capitals). That rules out email addresses and names. Nested objects, lists and null are rejected.

`at` (when it happened on the phone) is optional. It is kept if it is within the last 24 hours, pulled back to "now" if the phone's clock is ahead, and pulled up to 24 hours ago if older.

The player id always comes from the signed-in session, never from the request body.

### Adding a new event

1. Add the name to `AnalyticsEventNames` in `internal/dto/analytics_dto.go` (and update the count in its test).
2. Add a query for it to `analytics/reports.sql` in the migration repo.
3. Send it from the app.

## Deploy order

1. Merge the migration `000008_analytics_events` into the migration repo's `main` (its GitHub Action applies it to the database).
2. Deploy the backend.

If the backend is deployed first, nothing breaks: events are accepted and quietly not stored until the table exists.

## Privacy

Events hold the player id, an event name, a timestamp and a few identifiers. Deleting a player deletes their events (`ON DELETE CASCADE`). If you publish a privacy policy, mention that anonymous usage events are recorded to improve the app.
