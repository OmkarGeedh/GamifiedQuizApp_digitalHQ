# Streak rewards

A bonus for reaching 3, 7, 14 and 30 days in a row, and a status endpoint that always shows the next milestone. Nothing in the app called these endpoints before, so adding them changes no existing behaviour.

## How the streak works today (unchanged)

The streak is counted in `GetOrCreateProfile` (`internal/services/profile_service.go`): the first time a player's profile is loaded on a new UTC day, the streak goes up by one if they were also active yesterday, otherwise it restarts at 1. Loading the profile happens when the app opens and at the start of every game flow, so **the streak counts days the player opened the app, not days they finished a game.**

Consequence: a "your streak is about to break" banner cannot be shown inside the app, because opening the app already saves the streak. A warning only makes sense as a reminder notification while the app is closed (not built yet; `day_ends_at` below is there for it). If you would rather count only days with a finished game, change `GetOrCreateProfile`; this feature then follows automatically.

## Milestones

Defined in `StreakMilestones` in `internal/services/streak_service.go`. Change the numbers there; nothing else needs to move.

| Days | Reward |
|---|---|
| 3 | 20 coins |
| 7 | 50 coins |
| 14 | 5 gems |
| 30 | 150 coins |

Each can be earned once **per streak**. If the streak breaks and the player builds a new one, the milestones can be earned again.

## Endpoints

Registered at the root and under `/api/v1`. Both need a signed-in player.

### `GET /streak`

```json
{
  "current": 7, "highest": 9,
  "day_ends_at": "2026-03-11T00:00:00Z",
  "next_milestone_days": 14, "days_to_next_milestone": 7,
  "milestones": [
    { "days": 3, "currency": "coins", "reward": 20, "reached": true, "claimed": true,  "claimable": false },
    { "days": 7, "currency": "coins", "reward": 50, "reached": true, "claimed": false, "claimable": true }
  ]
}
```

`next_milestone_days` is 0 once every milestone has been reached. Calling this also counts today as an active day, like any profile call.

### `POST /streak/claim`

Body: `{ "days": 7 }`. Rate limited to 30 per minute per player.

| Status | Meaning |
|---|---|
| 200 | Reward paid. Returns `days`, `currency`, `reward`, and the new `coins` and `gems` balances |
| 400 | Not a milestone, or the streak has not reached it yet |
| 409 | Already claimed for this streak |
| 401 | Not signed in |

## Streak freezes

A freeze protects the streak on a day the player misses. Players buy them with gems; one is used up automatically for each missed day, so a short break does not reset the streak.

| | |
|---|---|
| Price | 10 gems each (`StreakFreezePriceGems`) |
| Most held at once | 2 (`MaxStreakFreezes`), which is also the most missed days a streak can survive |
| Where it is stored | `clients.streak_freezes` (migration `000007_streak_freezes` in the migration repo) |

How it works, in `GetOrCreateProfile` (the same place the streak is counted):

- Opened yesterday: streak goes up, no freeze used (as before).
- Missed 1 or 2 days and holds at least that many freezes: that many are spent, the streak **continues** (+1 for today), and the missed days are marked active in the week calendar.
- Anything else (no freezes, not enough, or a break longer than 2 days): the streak restarts at 1 and **no freeze is spent**. Freezes are never used on a streak they cannot save.
- Spending is one SQL statement that also moves `last_active_date` to today, so two requests arriving together on a new day cannot both spend freezes.

### `POST /streak/freeze`

No body. Rate limited together with the claim endpoint.

| Status | Meaning |
|---|---|
| 200 | Bought. Returns the new `freezes` and `gems` |
| 400 | Not enough gems |
| 409 | Already holding the maximum |

`GET /streak` now also returns `freezes_enabled`, `freezes`, `max_freezes` and `freeze_price_gems`.

### Deploy order (important)

1. Apply migration `000007_streak_freezes` to the database.
2. Then deploy the backend.

`freeze_price_gems`, buying and spending all need the column. Until it exists nothing breaks: `GET /streak` returns `freezes_enabled: false` (so the app hides freezes) and a missed day restarts the streak as before. `streak_freezes` is deliberately not a field of `models.Profile`, because `UpdateProfile` saves the whole struct and every profile write would then fail without the column.

A known limitation, not new: profile writes save the whole row from a copy read earlier, so a very unlucky overlap with another write could put `last_active_date` back and let a freeze be spent twice for one gap. The window is tiny and it costs the player a freeze, not coins.

## How a claim is made safe

A claim is a `wallet_ledger` row with `transaction_type = 'streak_bonus'` and `reference_id = 'sm:<days>:<YYYYMMDD of the streak's first day>'`. "Claimed" is a lookup of that reference, and a new streak starts on a different day, so it gets different references. The claim runs in one transaction under a per-player advisory lock (see `internal/repo/reward_claim_repo.go`, shared with daily goals), and the streak is read again inside the lock, so a stale value cannot pay a milestone that was not reached.

## Tests

`go test ./internal/repo/ ./internal/services/ ./internal/routes/` covers the milestone rules, claiming once, a new streak claiming again, gems vs coins, eight simultaneous taps, and that streak and daily goal rewards do not block each other. As with daily goals, the PostgreSQL advisory lock itself is not exercised (no Postgres in that environment): verify one real double-tap against Postgres before relying on it.
