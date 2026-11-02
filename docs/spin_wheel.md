# Spin wheel

Server-authoritative weighted wheel. The client only animates to the `segment_code` the server returns.

## Endpoints (root and `/api/v1`, Bearer auth)

| Method | Path | Notes |
|---|---|---|
| GET | `/spin-wheel/segments` | Active segments in display order with `odds_percent` (published odds) |
| GET | `/spin-wheel/status` | `{spins}`: spins the player can still use |
| POST | `/spin-wheel/spin` | Requires `Idempotency-Key` (UUID). 409 = no spins, 400 = bad key, 503 = odds table invalid. Rate limited to 30/min per IP |

`POST /spin` returns `{segment_code, currency, amount, is_jackpot, spins_remaining, coins, gems, replayed}`.

## Odds

Table `spin_wheel_segments` (owned by `SkillVerse-SQL_Migration`, seed `60_spin_wheel.sql`). `weight_ppm` is parts per million (5000 = 0.5%); active weights must total exactly 1,000,000. The backend validates this on every load and returns 503 rather than pay from a bad table. The roll is a `crypto/rand` integer in `[0, 1_000_000)` walked over the cumulative weights in `sort_order`. Segments are cached in Redis only (1 min TTL); no Redis means a DB read.

Changing odds is a seed change: edit `60_spin_wheel.sql`, `make seed`. Placeholder wheel: 10/20/50/100 coins, 5/10 gems, jackpot 5000 coins at 0.5% (about 44.5 coins + 1.3 gems expected per spin).

## Atomicity and retries

`repo.SpendSpin` is one SQL statement: spend one spin (`spins > 0`), pay the prize, write `spin_results`, `wallet_ledger` (`spin_reward`) and `spin_ledger`. `UNIQUE (client_id, idempotency_key)` makes retries safe: a repeated key returns the stored result (`replayed: true`) and spends and pays nothing. Two requests racing on the same key roll back the loser and it returns the winner's result. The client keeps its key after a network failure and drops it on any definitive answer.

## Granting spins

Spins are earned. `services.GrantSpins(ctx, clientID, n, reason, referenceID)` is idempotent per `(client, reason, reference)`, so e.g. the game `session_id` can never grant twice. **No game event calls it yet**: which events grant spins and how many is a product decision.

## Tests

`internal/services/spin_wheel_service_test.go` (boundaries, validation, distribution) and `internal/handlers/spin_wheel_handler_test.go` (DB-backed: concurrency, idempotency, ledger, grants; skipped without the 000006 migration).
