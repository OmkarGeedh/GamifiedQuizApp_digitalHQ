# Sudden Death on Zoho Catalyst: WebSocket failure and move to HTTP

**Date:** 2026-10-29 · **Status:** Implemented · **Contract:** [sudden_death.md](sudden_death.md)

## Problem

Sudden Death could not connect in production (`https://quiz-api-50046981183.development.catalystappsail.in`).
The backend and app worked locally; only the Catalyst deployment failed.

## Findings

Probed the live deployment with the backend's diagnostic endpoint `GET /ws/echo`, which sends a
`connected` frame immediately after the upgrade.

| Check | Result |
|---|---|
| Plain HTTPS (`/health`) | `200` in ~0.1 s |
| WebSocket upgrade status | `HTTP/1.1 101` from Catalyst's edge (`Server: ZGS`) |
| `Upgrade: websocket` response header | **Missing** (only `Connection: upgrade`) |
| Strict client (gorilla/websocket; Dart `WebSocket` and browsers behave the same) | `websocket: bad handshake` |
| Raw client that ignores the missing header | **No frames delivered** in either direction; connection closed within ~30 s |
| Same server locally (`TestWebSocket_EchoDiagnostics`) | Works |

So the Catalyst AppSail edge acknowledges the upgrade but does not tunnel WebSocket traffic.
The Catalyst CLI (v1.28) exposes no AppSail proxy/WebSocket setting (only deploy, env, memory),
and Zoho's AppSail docs make no WebSocket support claim. The app's "proxy-compatible handshake"
fallback could not help because the frames never arrive.

## Options considered

| Option | Verdict |
|---|---|
| Fix/configure Catalyst | Not possible from our side: no setting exists, behaviour is in Zoho's edge. |
| Move the backend to a host with WebSocket support (Cloud Run, Fly.io, ECS + ALB) | Works, but means a migration, new costs, and losing Catalyst's free scale-to-zero, for one game mode. |
| **Run Sudden Death over HTTP** | **Chosen.** The game is turn-based (question → answer → verdict) and never needs server push. Works on any host, survives scale-to-zero and instance restarts (state lives on the Redis-backed session), and no infrastructure change. |

Cost of the choice: one HTTPS round-trip per answer (~100 ms on Catalyst) instead of a frame on an
open socket, which is invisible next to the 400 ms result animation. Timing stays server-authoritative.

## What changed

Backend (`GamifiedQuizApp_digitalHQ`):
- `POST /quiz/sudden-death/question | answer | power-up` (both `/` and `/api/v1` prefixes) in
  `internal/ws/turns.go`. Responses carry the same event payloads the socket used to push.
- Question deadline and `add_time` usage are stored on the cached `models.GameSession`
  (`question_deadline`, `add_time_used`). A request after the deadline (+1 s HTTP grace) resolves as timeout.
- Scoring (`scoreAnswer`) and finalization (`finalizeRun`) are shared by the socket and HTTP paths,
  so the rules exist once.
- Fixed: socket-path answer history never set `game_mode`; it was mislabelled `mcq`
  (and would fail against the new NOT NULL column).

Flutter (`Gamified-learning`, data layer only, no UI changes):
- `RestSuddenDeathClient` implements the existing `SuddenDeathSocketClient` contract, so the game
  controller and screens are untouched. It fetches the question on `join_game`, posts answers and
  power-ups, reports `timeout` when the server's remaining time runs out, and maps transport
  failures to `socket_error` so the existing reconnect/resync logic applies.
- The `dart:io` WebSocket client was removed.

## Still in place / follow-ups

- `GET /ws/game` and `/ws/echo` remain for other hosts and diagnostics; the app no longer uses them.
  Once no client depends on them, delete `internal/ws` socket code (`conn.go`, `manager.go`,
  `session.go`, socket handlers) and the WebSocket items in [deployment_checklist.md](deployment_checklist.md).
- Without Redis, session state is per-instance memory; if Catalyst ever runs more than one
  instance, configure `REDIS_URL` so every request sees the same session.

## Verify on Catalyst after deploy

```bash
B=https://quiz-api-50046981183.development.catalystappsail.in/api/v1
TOKEN=...   # POST $B/auth/login
S=$(curl -s -X POST $B/quiz/sessions/create -H "Authorization: Bearer $TOKEN" \
  -d '{"topic":"accounts","game_mode":"sudden_death","question_count":5,"abandon_stale":true}' | jq -r .data.session)
curl -s -X POST $B/quiz/sudden-death/question -H "Authorization: Bearer $TOKEN" -d "{\"session\":\"$S\"}" | jq .data.events
```
