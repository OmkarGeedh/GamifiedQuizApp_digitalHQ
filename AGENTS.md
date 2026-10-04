# AGENTS.md

Single-service Go backend for a gamified MCQ quiz app. Module: `github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ` (Go 1.25). Stack: Gin, GORM, PostgreSQL 16, Redis 7, gorilla/websocket.

## Commands

There is no `make build` / `make test`. Use Go directly:

```bash
go build ./...
go test ./...
go test ./internal/services/ -run TestCalculatePoints -v   # single test
```

`.github/workflows/go.yml` is the only CI: on push/PR to `main` it runs `go build -v ./...` then `go test -v ./...`. No lint, no formatter, no typecheck step, no pre-commit hooks.

`Makefile` targets:
- `make run` — `go run .`
- `make docker-up` / `docker-down` / `docker-logs`
- `make migrate|migrate-down|migrate-reset|migrate-seed|migrate-status` — these `docker compose exec app`, i.e. they **require the stack to already be running**.
- `make local-migrate|local-seed|local-status` — same actions on the host via `go run ./cmd/migrate -action=...`.

## Current build state

`go build ./...` **fails**; `go test ./...` reports `FAIL internal/mailer [build failed]` and passes every other package. Cause: an uncommitted working-tree edit to `internal/config/config.go` deletes the `SMTPConfig` struct and `Config.SMTP` field, while `internal/mailer/smtp.go:19` and `internal/mailer/smtp_test.go:25` still reference them. The tracked `.env` also lacks the `SMTP_*` keys that `.env.example` and `docker-compose.yml` define.

This reads as in-progress removal of SMTP email delivery. Confirm intent before "fixing" it by rewriting `internal/mailer` — the fix may be to delete the mailer package, not to restore the config.

## Docker: there is no hot reload

`docker-compose.yml` sets `command: ["./bin/server"]`, which **overrides** the Dockerfile's `CMD`. Combined with the `.:/app` bind mount, the container runs the prebuilt binary from your host working tree — not Air, despite the README and the Air install in `Dockerfile`.

```bash
go build -o bin/server .    # required before make docker-up
make docker-up
```

Rebuild and `docker compose restart app` after every code change. `.air.toml` (output `./tmp/main`) and the Air install are dead weight for the compose flow.

## Schema and migrations

Migrations are **GORM AutoMigrate**, not the `.sql` files. `migrations/*.sql` are legacy and unreferenced by any Go code. Schema is owned by `internal/models/*` and `db.AllModels()` in `internal/database/migration.go`.

When adding a model you must touch three places:
1. the struct in `internal/models/`
2. `AllModels()` in `internal/database/migration.go`
3. the hardcoded `tables` slice in `db.Status()` (same file) or it won't show up in `make migrate-status`

Managed tables: `clients`, `client_profiles`, `client_streak_activities`, `questions`, `game_sessions`, `user_question_history`, `wallet_ledger`.

`main.go` auto-migrates **and** auto-seeds on every boot when the `questions` table is empty. `seedQuestions()` issues `TRUNCATE TABLE questions RESTART IDENTITY CASCADE` before re-inserting from `internal/data/seed_questions.json` — running `-action=seed` destroys local question edits. Seed credentials: `player@example.com` / `secretpassword123`.

## Env loading quirks

- `config.LoadEnv()` walks `.env` from the CWD up to 4 parent dirs and uses `godotenv.Load`, which **never overrides** already-set variables.
- `DATABASE_URL` and `REDIS_ADDR` win outright over the discrete `POSTGRES_*` / `REDIS_HOST`+`REDIS_PORT` vars when non-empty.
- Docker Compose injects its own `DATABASE_URL` pointing at host `postgres`, so a stale `localhost` `DATABASE_URL` in `.env` is the usual cause of "connection refused" inside the container.
- `main.go` and several services read `os.Getenv("ENV")` / `os.Getenv("PORT")` **directly**, bypassing `config.AppConfig.Server`. Keep both paths in sync when changing these.

## Dual route registration

Every endpoint exists twice: once at the root (`/auth/...`, `/quiz/...`, `/profile/...`, `/wallet/...`) and once under `/api/v1`. Each `Register*Routes` in `internal/routes/` builds routes inside a closure and invokes it twice (`rootGrp := router.Group("")` and `v1Grp := router.Group("/api/v1")`).

Add new endpoints **inside the shared closure** so both prefixes get them. Registering directly on `router` breaks the mirror — `internal/routes/routes_test.go` asserts it. The only exception is the WebSocket route, which is explicitly registered at `/ws/game` and `/api/v1/ws/game`.

The CORS middleware is an inline anonymous func in `main.go` (it also strips trailing slashes to dodge 301/307 drops) and is **duplicated verbatim** in `internal/routes/routes_test.go`. Change both together. `r.HandleMethodNotAllowed = true` plus custom `NoRoute`/`NoMethod` give JSON 404/405 bodies.

## Redis is optional

`config.RedisClient` is a global set in `main.go`. If Redis is unavailable the app still boots, and:
- signup cooldowns / rate limits / pending signups fall back to the in-memory maps in `internal/services/signup_service.go`
- `middleware.RateLimit*` becomes a no-op (`config.RedisClient == nil` → `c.Next()`)

Never write code or tests that assume Redis is present. A stale `bin/server` can also be running while you test new code — check the running process.

## Scoring is deliberately fixed

`docs/points_calculation.md` is the authority. `CalculatePoints(basePoints, difficulty, timeTakenMs, comboStreak)` in `internal/services/game_service.go` **keeps its 4-arg signature but ignores every argument** and returns the `MCQCorrectAnswerPoints` constant. Do not reintroduce speed or combo multipliers.

Coins and XP are awarded only on session completion (`CalculateGameRewards`), not per answer. Level-up rewards are a separate economy event, excluded from `xp_awarded` / `coins_awarded` and their breakdowns.

Quiz sessions have a 5-minute inactivity TTL (`services.SessionInactivityTTL`), refreshed in Redis on activity. `services.StartSessionTTLSweeper`, started from `main.go`, runs every minute and marks stale sessions abandoned with no rewards.

## Layering conventions

`routes` → `handlers` (HTTP glue only) → `services` (business logic) → `repo` (GORM queries) → `models`.

- Services return `(result, http.Status, error)`. HTTP status codes are decided in the service layer, not the handler.
- All responses go through `internal/utils/response` (`Success` / `Error` / `ValidationError`), producing `{success, message, data, errors, timestamp}`.
- Request/response shapes live in `internal/dto` with `Validate()` methods for input.
- `repo.GetDB()` returns the global `config.DB`.
- Passwords are `bcrypt(password + PASSWORD_SECRET pepper)` — always go through the pepper.
- WebSocket auth passes the JWT as a `?token=` query param, not a header.

Tests are plain unit tests, no build tags, no DB or Redis fixtures required.

## Repo hygiene

- **No `.gitignore` exists**, and `.env`, `bin/server` (~48 MB), `tmp/main` (~45 MB), and `.DS_Store` are all tracked. Never use `git add -A`; stage paths explicitly.
- `.env` is committed and holds real secrets (`JWT_SECRET`, `PASSWORD_SECRET`). Do not paste its contents into commits, PR descriptions, or docs.
- Work happens on `main` directly.
- `docs/auth.md` is an empty file (0 bytes) despite being linked from the README.

## Stale documentation

The README is out of date in ways that will waste your time:
- Its Postman walkthrough uses `POST /auth/login/otp` → `/auth/login/verify-otp`. Those routes were **removed**; login is `POST /auth/login` with email + password. `TestLogin_OTPRouteIsRemoved` in `internal/handlers/auth_handler_test.go` asserts they 404.
- It claims the compose stack runs with Air live reload (see Docker section above).
- It references a `postman/` directory; the actual directory is `Postman/`.

`Postman/README.md` also describes speed and combo scoring multipliers that the backend no longer implements.