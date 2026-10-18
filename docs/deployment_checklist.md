# Backend Infrastructure & Production Deployment Checklist

This document is the authoritative checklist for deploying the Gamified Quiz App backend to any cloud environment (Zoho Catalyst, AWS, GCP, Render, Fly.io, etc.). Whenever new infrastructure or deployment targets are added, **every item in this checklist must be audited and verified**.

---

## 📋 The 7-Pillar Pre-Deployment Checklist

```mermaid
flowchart TD
    P1["1. Database & DDL Migrations\n- External Postgres 16\n- sslmode=require\n- Seeded via SkillVerse-SQL_Migration"] --> P2["2. Redis / Cache Strategy\n- Upstash / Cloud Redis\n- In-Memory Fallback Verified\n- REDIS_URL with TLS"]
    P2 --> P3["3. WebSocket Ingress & Upgrades\n- Direct Container Routing\n- Bypass Incompatible API Gateways\n- Enforce wss:// TLS"]
    P3 --> P4["4. Proxy Timeouts & Heartbeats\n- Edge Proxy Timeout Checked\n- PingInterval < Proxy Timeout\n- TCP Keep-Alive Enabled"]
    P4 --> P5["5. Handshake & Auth Parameters\n- JWT in Query Param (?token=)\n- Session Precondition Created\n- CORS / CheckOrigin Permissive"]
    P5 --> P6["6. Port Binding & Graceful Exit\n- Dynamic PORT / Listen Port\n- < 10s Cold Start Boot\n- Graceful SIGTERM Handling"]
    P6 --> P7["7. Scale-to-Zero & Session Safety\n- Ephemeral Instance Recovery\n- Session Inactivity Sweeper\n- Reconnect & Resync Handlers"]
```

---

### Pillar 1: Database & Migrations
- [ ] **External Managed Postgres**: Ensure a reliable PostgreSQL 16 instance is provisioned (Neon, Supabase, AWS RDS).
- [ ] **TLS Enforcement**: `DATABASE_URL` must have `?sslmode=require` (or `POSTGRES_SSLMODE=require`).
- [ ] **DDL & Migration Separation**: The Go backend does **not** run migrations on boot. Apply pending schema migrations and seed questions before booting the application:
  ```bash
  cd SkillVerse-SQL_Migration
  DB_URL="postgresql://user:pass@host:5432/dbname?sslmode=require" ./bin/game-db migrate up
  DB_URL="postgresql://user:pass@host:5432/dbname?sslmode=require" ./bin/game-db seed -format all
  ```

---

### Pillar 2: Redis Cache Strategy
- [ ] **Verify Cloud Provider Capabilities**: Check whether the cloud platform offers standard TCP Redis. (Zoho Catalyst's internal cache does *not* offer raw TCP port 6379; AWS ElastiCache, Upstash, and Redis Cloud do).
- [ ] **Provision Redis**:
  - **Zero-Cost / In-Memory Fallback**: If using a single small instance, leave `REDIS_ADDR` empty. The app safely uses in-memory `sync.Map`.
  - **Cloud Serverless Redis (Recommended for Production)**: Create a free [Upstash Redis](https://upstash.com) database and supply:
    ```env
    REDIS_URL=rediss://default:YOUR_PASSWORD@your-db.upstash.io:6379
    ```
- [ ] **Boot Log Audit**: Confirm server logs output `Redis connection established successfully` (or fallback warning if intended).

---

### Pillar 3: WebSocket Ingress & Routing
- [ ] **Protocol Switch Support**: Verify that the fronting reverse proxy / load balancer forwards `Upgrade: websocket` and `Connection: Upgrade` headers (`101 Switching Protocols`).
- [ ] **Bypass Stateless API Gateways**: If an edge API Gateway does not support WebSocket hops (e.g. Zoho Catalyst API Gateway), route WebSocket clients directly to the persistent container subdomain (e.g. `*.catalystappsail.com`).
- [ ] **Enforce TLS (`wss://`)**: On HTTPS production domains, ensure frontend clients dial `wss://` instead of `ws://` to prevent browser mixed-content blocks.
- [ ] **Dual Route Registration**: Confirm routes exist on both `/ws/game` and `/api/v1/ws/game`.

---

### Pillar 4: Proxy Timeouts & Keep-Alive Pings
- [ ] **Identify Edge Proxy Timeout**: Check the host's request/connection idle threshold:
  - **Zoho Catalyst AppSail**: 30 seconds
  - **AWS ALB**: 60 seconds (default)
  - **Cloudflare**: 100 seconds
- [ ] **Verify Ping Frequency**: Ensure server `PingInterval` is strictly less than the proxy timeout:
  - Catalyst: `PingInterval = 15s`, `PongWait = 30s`
  - AWS/GCP: `PingInterval = 54s`, `PongWait = 60s`
  - Use `CLOUD_PROVIDER=<zoho|aws|gcp>` or manual overrides:
    ```env
    WS_PING_INTERVAL_SECONDS=15
    WS_PONG_WAIT_SECONDS=30
    WS_TCP_KEEPALIVE_SECONDS=15
    ```

---

### Pillar 5: Handshake Authentication & Preconditions
- [ ] **Query Parameter Tokens**: Web browsers cannot send custom headers during WebSocket handshakes. Confirm query param authentication accepts:
  - `?token=<jwt>`
  - `?access_token=<jwt>`
  - `?accessToken=<jwt>`
- [ ] **Session Precondition**: A quiz session must be initialized via REST (`POST /api/v1/quiz/sessions/create`) before connecting to the WebSocket with `?session_id=<UUID>`.

---

### Pillar 6: Container Lifecycle & Port Binding
- [ ] **Dynamic Port Resolution**: The application must bind to `PORT` or provider-specific vars (e.g. `X_ZOHO_CATALYST_LISTEN_PORT`).
- [ ] **Fast Boot Time**: Container must boot and bind within platform thresholds (e.g. Catalyst terminates if port is not bound in 10s). Use multi-stage stripped builds ([`Dockerfile.catalyst`](../Dockerfile.catalyst)).
- [ ] **Graceful Shutdown**: The server must listen for `SIGTERM` / `SIGINT` and allow in-flight connections 5 seconds to drain before exit.

---

### Pillar 7: Scale-to-Zero & State Resilience
- [ ] **Idle Scaling Behavior**: Platforms like AppSail scale to zero after 5 minutes of inactivity.
- [ ] **Session Recovery**: If an instance recycles, active sessions stored in Redis or database are reloadable via `GetOrCreate` in `SessionManager`.
- [ ] **Session Sweeper**: Background sweeper ([`services.StartSessionTTLSweeper`](../internal/services/session_sweeper.go)) marks abandoned sessions (>5m inactivity) without leaking memory.

---

## 🛠 Cloud Platform Quick Reference

| Feature | Zoho Catalyst AppSail | AWS (ECS + ALB) | GCP Cloud Run | Render / Fly.io |
|---|---|---|---|---|
| **`CLOUD_PROVIDER` Env** | `zoho` | `aws` | `gcp` | `generic` |
| **Proxy Idle Timeout** | 30s | 60s (configurable) | 60m (stream) / 300s | 60s |
| **`WS_PING_INTERVAL`** | 15s | 54s | 54s | 25s - 54s |
| **Native Redis** | No (Use Upstash/Cloud) | ElastiCache / MemoryDB | Memorystore | Managed Redis add-on |
| **Port Variable** | `X_ZOHO_CATALYST_LISTEN_PORT` or `PORT` | `PORT` (8080) | `PORT` (8080) | `PORT` (8080) |
| **WebSocket URL** | `wss://<app>-<zaid>.<env>.catalystappsail.com` | `wss://api.yourdomain.com` | `wss://<service>.a.run.app` | `wss://<app>.onrender.com` |
