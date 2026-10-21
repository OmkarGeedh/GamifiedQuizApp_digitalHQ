# Infrastructure & Production Deployment Rules

> **DEPRECATION NOTICE**: Railway is no longer used or supported for backend hosting. Never propose, generate configurations for, or deploy to Railway (`*.up.railway.app`). The current production/staging platform is Zoho Catalyst AppSail.

Whenever planning, implementing, or modifying backend deployment infrastructure for this repository (such as Zoho Catalyst, AWS, GCP, etc.), you MUST audit and adhere to the 7-Pillar Deployment Checklist defined in `docs/deployment_checklist.md`.

## The 7 Mandatory Verification Pillars

1. **Database & Migrations**:
   - PostgreSQL 16 external database MUST enforce `sslmode=require`.
   - Never run DDL migrations or content seeding on server boot. All schema changes and seeds are authoritatively managed by `SkillVerse-SQL_Migration` CLI before deploying backend containers.

2. **Redis & Cache Architecture**:
   - Check if the target platform provides native TCP Redis sockets (port 6379).
   - If deploying to a platform without direct TCP Redis (like Zoho Catalyst), use an external managed serverless Redis (e.g. Upstash via `REDIS_URL=rediss://...`) or verify that the app's zero-cost in-memory `sync.Map` fallback operates without crashing.

3. **WebSocket Ingress & Protocol Switching**:
   - Verify that the reverse proxy / load balancer forwards `Upgrade: websocket` and `Connection: Upgrade` headers (`101 Switching Protocols`).
   - If an edge API Gateway drops WebSocket upgrade headers (like Zoho Catalyst API Gateway), route WebSocket traffic directly to the container subdomain (e.g., `*.catalystappsail.com`).
   - Always enforce TLS (`wss://`) on HTTPS production domains.

4. **Proxy Idle Timeouts & Keep-Alive Pings**:
   - Determine the edge proxy idle timeout (e.g. Zoho Catalyst AppSail = 30s, AWS ALB = 60s).
   - The server's `PingInterval` MUST be strictly less than the proxy idle timeout (e.g. 15s for Catalyst, 54s for AWS).
   - Configure via `CLOUD_PROVIDER=<zoho|aws|gcp>` or explicit environment variables (`WS_PING_INTERVAL_SECONDS`, `WS_PONG_WAIT_SECONDS`, `WS_TCP_KEEPALIVE_SECONDS`).

5. **Authentication & Handshake Query Parameters**:
   - Browser WebSockets cannot send custom HTTP headers during the handshake. Handshake authentication must accept `?token=`, `?access_token=`, or `?accessToken=`.
   - Ensure mode-specific game sessions are created via REST (`POST /api/v1/quiz/sessions/create`) before dialing the WebSocket with `?session_id=`.

6. **Container Lifecycle & Port Binding**:
   - The server must bind dynamically to `PORT` or platform-specific variables (e.g. `X_ZOHO_CATALYST_LISTEN_PORT`).
   - Cold starts must complete and bind the listening port within platform limits (e.g. Catalyst's 10-second kill threshold).
   - Graceful shutdown must intercept `SIGTERM` and `SIGINT` with a 5-second drain window.

7. **Scale-to-Zero State Safety**:
   - In platforms with automatic scale-to-zero (e.g. Catalyst AppSail 5-minute inactivity), ensure that active sessions in memory are cleanly restored from Redis or database upon player reconnect via `SessionManager.GetOrCreate()`.
