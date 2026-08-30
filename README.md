# 🎮 Gamified Quiz App - Backend API

A high-performance, production-ready REST API backend for a **Gamified Quiz Application** built with **Go (Golang)**, **Gin Web Framework**, **GORM**, **PostgreSQL 16**, **Redis 7**, and containerized with **Docker Compose & Air (Live Reloading)**.

---

## 🏗️ Tech Stack & Key Features

- **Language & Framework**: [Go 1.24+](https://golang.org/) & [Gin Web Framework](https://gin-gonic.com/)
- **Database & ORM**: [PostgreSQL 16](https://www.postgresql.org/) with [GORM](https://gorm.io/) (Connection pool tuned)
- **In-Memory Cache & Lockout**: [Redis 7](https://redis.io/) for OTPs (1-min TTL, 30s cooldown), Failed Attempt Lockouts (5 attempts / 10 min), JWT Revocation/Blacklist, and Rate Limiting
- **Database GUI**: [pgAdmin 4](https://www.pgadmin.org/) containerized on port `5050`
- **Development & Live Reloading**: [Air](https://github.com/air-verse/air) for instant hot-reload inside Docker
- **Authentication**: 2FA OTP Login, 30-Day Access Token, 6-Month Refresh Token, HTTP-Only SameSite Cookies (`clientAccessToken`, `clientRefreshToken`), Bcrypt password hashing + Pepper Secret
- **Gamification Engine**: Gems, Coins, XP & Level progression, Weekly Rank, and 7-Day Streak Calendar Tracker

---

## ⚡ Prerequisites

Make sure you have installed on your machine:
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (Docker Engine + Docker Compose)
- [Make](https://www.gnu.org/software/make/) *(Pre-installed on macOS/Linux)*
- *(Optional for direct host running)* [Go 1.24+](https://golang.org/dl/)

---

## 🚀 Quick Start (Docker - Recommended)

### 1. Clone & Setup Environment

Copy the example environment file:
```bash
cp .env.example .env
```

### 2. Start the Application & Services

Run all containers (Go API with Air, PostgreSQL, Redis, pgAdmin) in the background:
```bash
make docker-up
# or: docker compose up -d
```

### 3. Check Service Health

```bash
docker compose ps
```
You should see 4 healthy services running:
- `quiz_api` on port `8080`
- `quiz_postgres` on port `5432`
- `quiz_redis` on port `6379`
- `quiz_pgadmin` on port `5050`

### 4. View Live Logs

```bash
make docker-logs
# or: docker compose logs -f app
```

---

## 🗄️ Database Migrations & Seeding

The application includes automated GORM migrations on startup, plus a dedicated CLI migration tool accessible via `Makefile`:

| Command | Description |
| :--- | :--- |
| **`make migrate`** | Runs auto-migration for `clients`, `client_profiles`, and `client_streak_activities` tables. |
| **`make migrate-status`** | Displays table existence check and live row counts. |
| **`make migrate-seed`** | Seeds an initial demo user (`player@example.com` / `secretpassword123`) with 250 coins, 50 gems, Level 2, and 3-day active streak. |
| **`make migrate-reset`** | Drops all tables and re-applies migrations cleanly. |
| **`make migrate-down`** | Drops all application tables. |

---

## 🖥️ pgAdmin 4 Web GUI Setup

pgAdmin 4 runs automatically on **http://localhost:5050**.

### 1. Log into pgAdmin
- **URL**: [http://localhost:5050](http://localhost:5050)
- **Email**: `admin@skillverse.com`
- **Password**: `admin`

### 2. Register Database Server
1. Click **Add New Server** (or right-click *Servers* &rarr; *Register* &rarr; *Server...*).
2. **General Tab**:
   - **Name**: `Gamified Quiz DB`
3. **Connection Tab**:
   - **Host name/address**: `postgres` *(Docker network service name)*
   - **Port**: `5432`
   - **Maintenance database**: `gamifiedapp`
   - **Username**: `skillverse`
   - **Password**: `skillverse_secrets`
   - Check **Save password?** &rarr; `Yes`
4. Click **Save**.

---

## 🔐 Authentication & Session Flow

### Token Validity & Storage

| Token | Validity | Storage | Header / Cookie |
| :--- | :--- | :--- | :--- |
| **Access Token** | **30 Days** | HTTP-Only Cookie (`SameSite=Lax; Path=/`) or Header | `clientAccessToken` / `Authorization: Bearer <token>` |
| **Refresh Token** | **6 Months** | HTTP-Only Cookie (`SameSite=Lax; Path=/`) & PostgreSQL DB | `clientRefreshToken` |

### Two-Factor Authentication (OTP)
1. **Request Login OTP**: `POST /auth/login/otp` with email and password.
2. **Retrieve OTP**: Logged prominently in terminal (`make docker-logs`) and stored in Redis with 1-min expiration.
3. **Verify OTP**: `POST /auth/login/verify-otp`. Returns access token and sets HTTP-only cookies.
4. **Logout**: `POST /auth/logout` revokes access token JTI in Redis blacklist and clears cookies.

---

## 🏆 Profile & Gamification API

All `/profile` endpoints require authentication (via cookie or Bearer token):

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/profile` | Fetch player profile with UUID, display name, email, phone, streaks, 7-day calendar history, gems, level, experience, coins, and weekly rank. *(Auto-provisions starter 100 coins & 10 gems on first visit)* |
| `POST` | `/profile` | Explicit initial profile setup / onboarding. |
| `PUT` | `/profile` | Update profile fields (display name, phone number, avatar URL). |

### Standard Response Envelope

```json
{
  "success": true,
  "message": "Profile fetched successfully",
  "data": {
    "uuid": "5a4daa02-af64-49bd-be6c-cb48ff87a939",
    "name": "Pro Quiz Master",
    "email": "player@example.com",
    "phone": "+1-555-0199",
    "avatarUrl": "",
    "streaks": 1,
    "highestStreak": 1,
    "last7DaysStreak": [
      { "date": "2026-08-24", "day": "Mon", "completed": false },
      { "date": "2026-08-25", "day": "Tue", "completed": false },
      { "date": "2026-08-26", "day": "Wed", "completed": false },
      { "date": "2026-08-27", "day": "Thu", "completed": false },
      { "date": "2026-08-28", "day": "Fri", "completed": false },
      { "date": "2026-08-29", "day": "Sat", "completed": false },
      { "date": "2026-08-30", "day": "Sun", "completed": true }
    ],
    "gems": 10,
    "coins": 100,
    "level": 1,
    "experience": 0,
    "nextLevelExp": 100,
    "weeklyScore": 0,
    "weeklyRank": 1,
    "stats": {
      "coins": 100,
      "gems": 10,
      "level": 1,
      "experience": 0,
      "nextLevelExperience": 100,
      "weeklyScore": 0,
      "weeklyRank": 1
    },
    "streak": {
      "current": 1,
      "highest": 1,
      "history": [...]
    },
    "createdAt": "2026-08-30T14:05:51Z",
    "updatedAt": "2026-08-30T14:05:57Z"
  },
  "timestamp": "2026-08-30T14:20:59Z"
}
```

---

## 📬 Postman Collections

Pre-configured Postman collection files are located in the [`postman/`](./postman/) directory:

- **`postman/GamifiedQuizApp.postman_environment.json`**: Environment variables (`baseUrl`, `accessToken`, `refreshToken`, `otp`, `resetToken`).
- **`postman/Auth.postman_collection.json`**: Auth & Registration collection.
- **`postman/Profile.postman_collection.json`**: Profile & Gamification collection.
- **`postman/GamifiedQuizApp_Full.postman_collection.json`**: Unified Master Collection.

> **Tip**: `POST /auth/login/verify-otp` has an automatic test script that extracts and stores `accessToken` and `refreshToken` directly into your Postman environment for instant use.

---

## 🛠️ Complete Makefile Reference

| Command | Action |
| :--- | :--- |
| `make docker-up` | Starts all containers in the background. |
| `make docker-down` | Stops and removes all containers. |
| `make docker-logs` | Follows real-time container log output. |
| `make migrate` | Executes GORM database auto-migrations inside Docker. |
| `make migrate-status` | Displays table health and total row counts. |
| `make migrate-seed` | Seeds initial demo player, profile, and streaks. |
| `make migrate-reset` | Drops and re-creates all database tables. |
| `make run` | Runs Go backend directly on local host. |
| `make local-migrate` | Runs migration tool directly against local host Postgres. |
