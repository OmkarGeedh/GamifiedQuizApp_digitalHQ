# Zoho Catalyst Deployment & Implementation Guide

This guide details how to deploy the **Gamified Quiz App Go Backend** on **Zoho Catalyst** using **AppSail (Custom Docker Runtime)** while maximizing performance, minimizing credit usage, and staying within free-tier limits.

---

## 1. Architecture Overview

```mermaid
flowchart TD
    Client["Clients\n(HTTP / WebSocket)"] -->|"Inbound Traffic"| AppSail

    subgraph Catalyst ["Zoho Catalyst Cloud Platform"]
        subgraph AppSail ["AppSail Container Service"]
            Service["Go 1.25 Web Service\n- 256 MB RAM Sizing\n- Stripped Binary (~25MB Image)\n- 5-Min Scale-to-Zero\n- Dynamic Port Binding"]
            MemoryCache[("In-Memory Session & Cache Fallback\n(sync.Map - $0.00 / 0 Credits)")]
            Service <--> MemoryCache
        end
    end

    subgraph External ["External Managed Free Tiers"]
        Postgres[("PostgreSQL 16 Database\n(Neon / Supabase Free Tier)\nsslmode=require")]
        Redis[("Redis 7 Cache (Optional)\n(Upstash Free Tier)")]
    end

    Service -->|"Outbound TLS (5432)"| Postgres
    Service -.->|"Optional TLS (6379)"| Redis
```

### Why AppSail?
- Standard Catalyst Serverless Functions only natively support Node.js, Python, and Java, and do not support long-lived WebSocket connections (`/ws/game`).
- **AppSail** allows deploying custom OCI Docker containers, providing full support for compiled **Go binaries**, high-throughput **Gin HTTP routing**, and low-latency **gorilla/websocket** connections.

---

## 2. Credit-Saving Strategy & Billing Breakdown

AppSail pricing is calculated on **GB-Minutes** of container uptime:

$$\text{Compute Cost} = \text{Allocated Memory (GB)} \times \text{Uptime (Minutes)} \times \$0.0013$$

| Component | Default Cloud Setup | Our Optimized Setup | Savings / Benefit |
|---|---|---|---|
| **Memory Allocation** | 1024 MB (1 GB) | **256 MB (0.25 GB)** | **75% reduction** in GB-minute burn |
| **Container Size** | 550 MB+ (Full SDK) | **~25 MB** ([`Dockerfile.catalyst`](../Dockerfile.catalyst)) | Starts in **< 300ms**, avoids 10s kill timeout |
| **Idle Behavior** | Always-on 24/7 | **Scale-to-Zero** after 5m inactivity | **0 credits charged** during idle hours |
| **Database** | Catalyst Data Store | **External Free PostgreSQL** (Neon/Supabase) | **0 Catalyst DB credits** consumed |
| **Cache** | Catalyst Cache | **In-Memory Fallback** or Upstash | **0 Catalyst cache credits** consumed |
| **Logging** | Debug per-request logs | `GIN_MODE=release`, `ENV=production` | **90% reduction** in log ingestion credits |

---

## 3. Production Dockerfile ([`Dockerfile.catalyst`](../Dockerfile.catalyst))

AppSail requires that your container starts and binds to the listening port within **10 seconds**, otherwise it is terminated and rebooted.

We provide a dedicated multi-stage build in [`Dockerfile.catalyst`](../Dockerfile.catalyst):

```dockerfile
# Stage 1: Build binary
FROM golang:1.24-alpine AS builder

WORKDIR /build
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build stripped static binary for Linux AMD64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /build/bin/server .

# Stage 2: Minimal runtime image (~25MB)
FROM alpine:3.20

WORKDIR /app

# ca-certificates for outbound TLS, tzdata for accurate timestamps
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/bin/server /app/server

EXPOSE 8080

CMD ["/app/server"]
```

---

## 4. Port & Startup Compatibility

Zoho Catalyst dynamically injects the port via `X_ZOHO_CATALYST_LISTEN_PORT` or standard `PORT`. 

The backend in [`main.go`](../main.go) is pre-configured to detect both:

```go
port := os.Getenv("PORT")
if port == "" {
    port = os.Getenv("X_ZOHO_CATALYST_LISTEN_PORT")
}
if port == "" {
    port = "8080"
}
```

---

## 5. Environment Variables Configuration

Set these environment variables in the **Zoho Catalyst Console** under **AppSail $\rightarrow$ Configuration $\rightarrow$ Environment Variables** (or use [`.env.production.example`](../.env.production.example)):

| Variable | Recommended Value | Notes |
|---|---|---|
| `ENV` | `production` | Enables production security and error formatting |
| `GIN_MODE` | `release` | Disables debug printing to save log credits |
| `DATABASE_URL` | `postgresql://user:pass@ep-xyz.neon.tech/dbname?sslmode=require` | External free PostgreSQL connection |
| `POSTGRES_SSLMODE` | `require` | Enforces TLS database encryption |
| `REDIS_ADDR` | *(leave empty or Upstash URL)* | If empty, backend uses zero-cost in-memory `sync.Map` |
| `REDIS_PASSWORD`| *(optional)* | Only needed if using external Redis |
| `JWT_SECRET` | *`<strong-random-key>`* | Auth token signing secret |
| `PASSWORD_SECRET`| *`<strong-pepper-key>`* | Password hash pepper salt |
| `CORS_ALLOWED_ORIGINS`| `https://quiz.yourdomain.com` | Restricts cross-origin requests in production |

> [!TIP]
> **Zero-Cost Redis**: Leaving `REDIS_ADDR` blank activates the app's resilient in-memory session and cache fallback. For low-to-medium single-instance workloads, this costs **$0.00** and consumes zero external network bandwidth.

---

## 6. Step-by-Step Deployment Walkthrough

### Prerequisites
1. Install Zoho Catalyst CLI:
   ```bash
   npm install -g zcatalyst-cli
   ```
2. Log in to your Zoho account:
   ```bash
   catalyst login
   ```

---

### Method A: Deploy via Docker Hub / Container Registry (Recommended)

1. **Build and push the multi-stage image for `linux/amd64`**:
   ```bash
   docker build -t your-dockerhub-username/quiz-backend:latest -f Dockerfile.catalyst --platform linux/amd64 .
   docker push your-dockerhub-username/quiz-backend:latest
   ```

2. **Configure in Catalyst Console**:
   - Go to **Catalyst Console $\rightarrow$ Your Project $\rightarrow$ AppSail**.
   - Click **Create AppSail Service**.
   - **Service Name**: `quiz-backend`
   - **Runtime Type**: `Custom Runtime (Docker Image)`
   - **Source**: Select your Container Registry (Docker Hub, AWS ECR, or GCP Artifact Registry).
   - **Image URI**: `your-dockerhub-username/quiz-backend:latest`
   - **Memory**: Select **256 MB** (or 512 MB).
   - **Port**: Set to `8080`.
   - Add the Environment Variables from Section 5.
   - Click **Deploy**.

---

### Method B: Deploy via Catalyst CLI

1. **Initialize Catalyst in your project directory**:
   ```bash
   catalyst init
   # Select your Project
   # Check "AppSail"
   ```

2. **Add AppSail Service**:
   ```bash
   catalyst appsail:add
   # Runtime: Custom Runtime / Docker Image
   # Select your Dockerfile: Dockerfile.catalyst
   ```

3. **Deploy**:
   ```bash
   catalyst deploy
   ```

---

## 7. Database Migrations & Seeding in Production

Database schema DDL, SQL migrations, and game content seeds are **strictly and authoritatively owned by the [`SkillVerse-SQL_Migration`](../../SkillVerse-SQL_Migration) repository**.

The AppSail Go web server is decoupled from database DDL and does not run schema migrations or seeds on boot.

### Production Migration Strategy
1. **Apply Migrations Before Deploying AppSail**:
   Before deploying or restarting the AppSail container, apply pending database migrations using `SkillVerse-SQL_Migration`:
   - **Via GitHub Actions (Automated CI/CD)**: Trigger the `Deploy Database` workflow (`.github/workflows/database-deploy.yml`) in `SkillVerse-SQL_Migration` with target `DATABASE_URL`.
   - **Via CLI (Direct)**:
     ```bash
     cd SkillVerse-SQL_Migration
     DB_URL="postgresql://user:pass@host:5432/dbname?sslmode=require" ./bin/game-db migrate up
     DB_URL="postgresql://user:pass@host:5432/dbname?sslmode=require" ./bin/game-db seed -format all
     ```
2. **Deploy AppSail Service**:
   Once the database schema and seeds are verified, deploy the Go backend container to AppSail.

---

## 8. WebSocket & Scale-to-Zero Best Practices

AppSail automatically scales down instances to **0** after 5 minutes of no activity, charging **zero compute credits**.

To ensure persistent WebSockets don't keep the instance awake indefinitely:

1. **Client Disconnect on Finish**:
   In your client application, explicitly close the WebSocket when a quiz concludes:
   ```javascript
   if (socket && socket.readyState === WebSocket.OPEN) {
       socket.close(1000, "Quiz Completed");
   }
   ```

2. **Client Disconnect on Tab Hide**:
   Close connections when players minimize or leave the browser tab:
   ```javascript
   document.addEventListener("visibilitychange", () => {
       if (document.visibilityState === "hidden" && socket) {
           socket.close(1000, "Tab Hidden");
       }
   });
   ```

3. **Automated Server Sweeper**:
   The backend's built-in [`services.StartSessionTTLSweeper`](../internal/services/session_sweeper.go) runs every 60 seconds and terminates sessions with >5 minutes of player inactivity.

---

## 9. Troubleshooting & FAQ

### Q1: The AppSail container failed with "Port not bound in 10 seconds"
- **Cause**: The container image is too large, or database connection initialization timed out.
- **Fix**: Verify you are using [`Dockerfile.catalyst`](../Dockerfile.catalyst) (~25 MB) and check that your `DATABASE_URL` is reachable from external networks with `sslmode=require`.

### Q2: How do I test the app locally before deploying?
- Run directly on host:
  ```bash
  go run .
  # or: make run
  ```
- Or run via Docker Compose without Air:
  ```bash
  docker compose restart app
  ```

### Q3: How do I verify the database status on Catalyst?
Use the migration CLI from `SkillVerse-SQL_Migration`:
```bash
cd SkillVerse-SQL_Migration
DB_URL="postgresql://user:pass@host:5432/dbname?sslmode=require" ./bin/game-db migrate version
```
