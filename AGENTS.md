# AGENTS.md — GamifiedQuizApp_digitalHQ (Go backend)

Single-service Go backend for the SkillVerse quiz app. Module `github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ` (Go 1.25). Stack: Gin, GORM, PostgreSQL 18, Redis 7 (optional). Hosted on **Zoho Catalyst AppSail** (Railway is decommissioned; never use `*.up.railway.app`).

## Commands

```bash
go build ./...
go test ./...                                   # DB-backed tests skip if Postgres is unreachable
go test ./internal/services/ -run TestCalculatePoints -v
make run                                        # go run .
make docker-up / docker-down / docker-logs
```

CI (`.github/workflows/go.yml`): `go build -v ./...` then `go test -v ./...` on push/PR to `main`. No lint step.

### Running DB-backed tests for real
Handler tests in `internal/handlers` need Postgres (and Redis for logout revocation). Use the migration repo's local stack:
```bash
cd ../SkillVerse-SQL_Migration && POSTGRES_PORT=5434 docker compose up -d postgres
go run ./cmd/db migrate up -db "postgresql://skillverse:skillverse_secrets@127.0.0.1:5434/gamifiedapp?sslmode=disable"
go run ./cmd/db seed       -db "<same url>"
docker run -d --rm --name sv_test_redis -p 6380:6379 redis:7-alpine
cd ../GamifiedQuizApp_digitalHQ
DATABASE_URL="<same url>" REDIS_ADDR=127.0.0.1:6380 go test ./... -count=1
```
Tests create their own content via `internal/handlers/content_fixture_test.go` (throwaway subject, removed by cascade). Never point tests at production. Note: `docker compose` in the migration repo reads `DB_URL` from its `.env` (Neon) — pass `-db` explicitly for local runs.

## Database (owned by SkillVerse-SQL_Migration)

The backend never migrates or seeds. Schema, content CSVs and seeds live in `../SkillVerse-SQL_Migration` (see its AGENTS.md); `make migrate-up && make seed` there.

Tables: `clients`, `client_profiles`, `client_streak_activities`, `wallet_ledger`, `subjects`, `chapters`, `mcq_questions`, `sudden_death_questions`, `sort_it_out_sets`, `sort_it_out_items`, `match_boards`, `match_pairs`, `user_question_history`, `leaderboard_game_results`. Game sessions live only in Redis (in-memory fallback).

- **Content is read only through views**: `v_mcq_questions`, `v_sudden_death_questions`, `v_sort_it_out_questions`, `v_match_boards`, `v_content_counts`. Models in `internal/models/game_model.go` map to them; never write content with GORM.
- API `topic_id` / `topic` = `subjects.code` (`accounts`, `economics`, `maths1`, `maths2`). Optional `chapter_no` narrows a session. `GET /subjects` returns subjects → chapters → per-mode counts (learning mode / topic picker).
- Question codes: `<subject short>-<ch>-<mode>-<n>`, ≤ 20 chars (e.g. `acc11-04-mcq-0012`); unique across all games, so `GetQuestionsByCodes` checks MCQ and Sudden Death views.
- Sort It Out `pair_type` = set code; Match pair IDs are derived as `<board id>-<pair order>`.
- `user_question_history.game_mode` is NOT NULL without default — always set `GameMode` on inserts.
- `leaderboard_game_results` is created by migrations; do not reintroduce `AutoMigrate`.
- Demo login: `player@example.com` / `secretpassword123` (seeded by the migration repo).

## Game modes and transport

All gameplay is HTTP. **Zoho Catalyst does not tunnel WebSockets** (101 without `Upgrade`, no frames; see `docs/catalyst_websocket_issue.md`).

| Mode | Flow |
|---|---|
| MCQ | `POST /quiz/sessions/create` → `/quiz/answers/evaluate` → `/quiz/sessions/complete` |
| Sort It Out | create → `/quiz/sort-it-out/complete` (batch) |
| Match the Following | create → `/quiz/match-the-following/complete` (batch) |
| Sudden Death | create → `/quiz/sudden-death/question` → `/answer` / `/power-up` (turns; contract in `docs/sudden_death.md`) |

Sudden Death HTTP turns live in `internal/ws/turns.go` and return ordered events (`question`, `answer_result`, `power_up_result`, `game_over`, `error`). Timing is server-authoritative via `question_deadline` / `add_time_used` on the cached session (+1 s HTTP grace). Scoring (`scoreAnswer`) and finalization (`finalizeRun`) in `internal/ws/session_handlers.go` are shared with the legacy socket game — keep rules in one place.

`/ws/game` and `/ws/echo` still exist but no client uses them; they are deletion candidates. Don't build new features on WebSockets while hosted on Catalyst.

## Scoring is deliberately fixed

`docs/points_calculation.md` is the authority. `CalculatePoints(...)` keeps its 4-arg signature but ignores every argument and returns `MCQCorrectAnswerPoints`. No speed/combo multipliers (Sudden Death's every-3rd-correct +5 streak bonus is the only exception). Coins/XP are awarded only on completion; level-up rewards are a separate event excluded from `xp_awarded` / `coins_awarded`.

Sessions have a 5-minute inactivity TTL (`services.SessionInactivityTTL`), refreshed on activity.

## Dual route registration

Every endpoint exists at the root and under `/api/v1`: each `Register*Routes` builds routes in a closure invoked for both groups. Add endpoints **inside the closure**; `internal/routes/routes_test.go` asserts the mirror. CORS is an inline func in `main.go` duplicated in `routes_test.go` — change both.

## Layering

`routes` → `handlers` (HTTP glue) → `services` (business logic, return `(result, status, error)`) → `repo` (GORM) → `models`. Responses via `internal/utils/response` (`{success, message, data, errors, timestamp}`). DTOs in `internal/dto` with `Validate()`. Passwords: bcrypt(password + `PASSWORD_SECRET` pepper).

## Env loading

- `config.LoadEnv()` reads `ENV`/`APP_ENV` (default `development`) and loads `.env.<env>.local` → `.env.<env>` → `.env.local` → `.env`, up to 4 parent dirs. Existing OS env always wins.
- `DATABASE_URL` / `REDIS_ADDR` override discrete `POSTGRES_*` / `REDIS_HOST`+`REDIS_PORT`.
- Redis is optional: without it rate limits are no-ops, signup cooldowns/sessions use in-memory maps, and logout token revocation does not work. Never assume Redis in code or tests. On Catalyst with >1 instance, Redis is required for shared session state.
- Production (`ENV=production`): Gin release mode, secure auth cookies.

## Deployment (Zoho Catalyst AppSail)

Audit `docs/deployment_checklist.md` before calling a deploy ready: external Postgres with `sslmode=require`; Redis via `rediss://` or in-memory fallback; bind `PORT` / `X_ZOHO_CATALYST_LISTEN_PORT` within 10 s; graceful `SIGTERM`; scale-to-zero safe session recovery. Image: `Dockerfile.catalyst`; config: `catalyst.json`; CLI: `catalyst deploy`.

Docker compose has no hot reload: it runs the prebuilt `bin/server`, so `go build -o bin/server .` before `make docker-up` and after every change.

## Repo hygiene

- No `.gitignore`: `.env` (real secrets), `bin/server`, `tmp/main`, `.DS_Store` are tracked. Never `git add -A`; stage paths explicitly and never paste `.env` contents anywhere.
- Work on `main`. **Never `git push`**; the user reviews and pushes.
- README/Postman are partly stale: OTP login routes were removed (`POST /auth/login` with email+password); Postman describes old scoring multipliers.
