# 🎮 Gamified Quiz App - Installation & Running Guide

This guide provides step-by-step instructions for installing, configuring, and running the **Gamified Quiz App Backend API** using Docker on **macOS**, **Linux**, and **Windows**.

---

## ⚡ Prerequisites

Before getting started, make sure you have installed:
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) *(for macOS, Windows, or Linux)*
- *(Optional for macOS/Linux)* `make`

---

## 🚀 1. Installation & Setup

### Step 1: Clone or Open the Project
Open your terminal (Terminal on macOS/Linux, or PowerShell / Command Prompt on Windows) in the project root directory:

```bash
cd GamifiedQuizApp_digitalHQ
```

### Step 2: Configure Environment Variables
Create your local `.env` file from the provided `.env.example`:

- **macOS / Linux**:
  ```bash
  cp .env.example .env
  ```
- **Windows (PowerShell)**:
  ```powershell
  Copy-Item .env.example .env
  ```
- **Windows (Command Prompt / CMD)**:
  ```cmd
  copy .env.example .env
  ```

---

## 🐳 2. Running with Docker Compose

### Step 1: Start All Services
Starts the Go API (with hot-reloading), PostgreSQL 16, Redis 7, and pgAdmin 4 in the background:

- **macOS / Linux (using Makefile)**:
  ```bash
  make docker-up
  ```
- **Windows / macOS / Linux (using Docker Compose)**:
  ```bash
  docker compose up -d
  ```

### Step 2: Verify Running Containers
Check that all 4 containers are running and healthy:

```bash
docker compose ps
```

| Service | Container Name | Port | Description |
| :--- | :--- | :--- | :--- |
| **`app`** | `quiz_api` | `http://localhost:8080` | Go Backend API (with Air live reload) |
| **`postgres`** | `quiz_postgres` | `localhost:5432` | PostgreSQL 16 Database |
| **`redis`** | `quiz_redis` | `localhost:6379` | Redis 7 In-Memory Cache |
| **`pgadmin`** | `quiz_pgadmin` | `http://localhost:5050` | pgAdmin 4 Database Web GUI |

### Step 3: View Live Server Logs
Watch the Go application logs and OTPs in real time:

- **macOS / Linux (using Makefile)**:
  ```bash
  make docker-logs
  ```
- **Windows / macOS / Linux (using Docker Compose)**:
  ```bash
  docker compose logs -f app
  ```

---

## 🗄️ 3. Database Migrations & Seeding

The application automatically runs migrations on startup. You can also run, inspect, or seed the database manually at any time:

### Run Migrations (`clients`, `client_profiles`, `client_streak_activities`)
- **macOS / Linux**:
  ```bash
  make migrate
  ```
- **Windows (PowerShell / CMD)**:
  ```bash
  docker compose exec app go run ./cmd/migrate -action=up
  ```

### Check Database Table Status & Row Counts
- **macOS / Linux**:
  ```bash
  make migrate-status
  ```
- **Windows (PowerShell / CMD)**:
  ```bash
  docker compose exec app go run ./cmd/migrate -action=status
  ```

### Seed Initial Demo User & Profile
Populates a test player (`player@example.com` / `secretpassword123`) with 250 coins, 50 gems, Level 2, and a 3-day active streak:

- **macOS / Linux**:
  ```bash
  make migrate-seed
  ```
- **Windows (PowerShell / CMD)**:
  ```bash
  docker compose exec app go run ./cmd/migrate -action=seed
  ```

### Reset Database (Drop & Re-Migrate)
- **macOS / Linux**:
  ```bash
  make migrate-reset
  ```
- **Windows (PowerShell / CMD)**:
  ```bash
  docker compose exec app go run ./cmd/migrate -action=reset
  ```

---

## 🖥️ 4. Accessing pgAdmin 4 Web GUI

1. Open your browser and navigate to: **[http://localhost:5050](http://localhost:5050)**
2. **Login Credentials**:
   - **Email**: `admin@skillverse.com`
   - **Password**: `admin`
3. **Connect to PostgreSQL Database**:
   - Right-click **Servers** &rarr; **Register** &rarr; **Server...**
   - **General Tab**:
     - Name: `Gamified Quiz DB`
   - **Connection Tab**:
     - Host name/address: `postgres` *(Docker container network host)*
     - Port: `5432`
     - Maintenance database: `gamifiedapp`
     - Username: `skillverse`
     - Password: `skillverse_secrets`
     - Save password: `Yes`
   - Click **Save**.

---

## 📬 5. Testing with Postman

Pre-configured Postman collection and environment files are available in the [`postman/`](./postman/) directory:

1. Open **Postman** &rarr; Click **Import** (top-left).
2. Select the files from the `postman/` directory:
   - `postman/GamifiedQuizApp_Full.postman_collection.json` (Unified Collection)
   - `postman/GamifiedQuizApp.postman_environment.json` (Environment)
3. Select the **"Gamified Quiz App (Local Docker)"** environment from the top-right dropdown in Postman.
4. **Login Flow**:
   - Run `POST /auth/login/otp` &rarr; Check terminal logs (`docker compose logs -f app`) for the generated OTP.
   - Run `POST /auth/login/verify-otp` &rarr; The test script automatically saves the `accessToken` into your Postman environment.
   - Run `GET /profile` to inspect your gamified profile, coins, gems, level, and 7-day streak history.

---

## 🛑 6. Stopping & Restarting the Application

### Stop All Containers
- **macOS / Linux (Makefile)**:
  ```bash
  make docker-down
  ```
- **Windows / macOS / Linux (Docker Compose)**:
  ```bash
  docker compose down
  ```

### Restart App Container (After Dependency Changes)
- **macOS / Linux / Windows**:
  ```bash
  docker compose restart app
  ```
