# Daily goals

Three goals per player per day. Finishing a goal lets the player claim a small reward once that day. Nothing in the app calls these endpoints yet, so adding them changes no existing behaviour.

## Goals

Defined in `DailyGoals` in `internal/services/daily_goals_service.go`. Change the numbers there; nothing else needs to move.

| id | Goal | Reward |
|---|---|---|
| `play_1` | Finish 1 game (any mode) | 20 coins |
| `play_3` | Finish 3 games (any mode) | 40 coins |
| `score_100` | Score 100 points across finished games | 2 gems |

The day is the **UTC day** (resets at 00:00 UTC, 05:30 IST), the same clock the streak calendar (`client_activity_days`) already uses.

## Endpoints

Registered at the root and under `/api/v1` (like every route). Both need a signed-in player.

### `GET /daily-goals`

```json
{
  "date": "2026-03-10",
  "resets_at": "2026-03-11T00:00:00Z",
  "goals": [
    { "id": "play_1", "title": "Finish 1 game", "target": 1, "progress": 1,
      "currency": "coins", "reward": 20,
      "completed": true, "claimed": false, "claimable": true }
  ]
}
```

`progress` is capped at `target`. `claimable` = completed and not yet claimed.

### `POST /daily-goals/claim`

Body: `{ "goal_id": "play_1" }`. Rate limited to 30 per minute per player.

| Status | Meaning |
|---|---|
| 200 | Reward paid. Returns `goal_id`, `currency`, `reward`, and the new `coins` and `gems` balances |
| 400 | Unknown `goal_id`, or the goal is not completed yet |
| 409 | Already claimed today |
| 401 | Not signed in |

## How it works

- **Progress** is counted from `leaderboard_game_results` (one row per finished game: `client_id`, `final_score`, `completed_at`) for the UTC day. No new table or migration is needed.
- **A claim** is a ledger row in `wallet_ledger` with `transaction_type = 'daily_goal_reward'` and `reference_id = 'dg:<goal_id>:<YYYYMMDD>'`. "Claimed today" is a lookup of that reference, and the reference changes at midnight, so the goal can be claimed again the next day.
- **Double claims.** The claim runs in one transaction. On PostgreSQL it first takes `pg_advisory_xact_lock(hashtext('reward_claim'), client_id)`, so two simultaneous taps from one player are serialized and the second sees the first one's ledger row. The same claim code (`internal/repo/reward_claim_repo.go`) also pays streak rewards (`docs/streak_rewards.md`). The lock is skipped on other databases (tests run on SQLite, which serializes on its single connection).
- The reward, the balance change and the ledger row are written together; if any step fails, nothing is paid.

## For the DB owner (optional)

The advisory lock is enough on its own. If you want a second safety net at the database level, add a partial unique index in the migration repo:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uq_wallet_ledger_daily_goal
    ON wallet_ledger (client_id, reference_id)
    WHERE transaction_type = 'daily_goal_reward';
```

The code works with or without it.

## Tests

`go test ./internal/repo/ ./internal/services/ ./internal/routes/` covers progress counting (own games only, day boundaries), paying once, gems vs coins, claiming again the next day, eight simultaneous taps, and refusing an unknown currency. The PostgreSQL advisory lock itself is not exercised by these (no Postgres in that environment); verify one real double-tap against a Postgres instance before relying on it.
