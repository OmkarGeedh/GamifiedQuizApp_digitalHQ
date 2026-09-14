# 📬 Postman Collections & Environment Guide

This directory contains the complete set of Postman collections and environment files for the **Gamified Quiz App API** (Go, Gin, GORM, PostgreSQL, Redis, Gorilla WebSocket).

---

## 📁 Files Included

| File | Description |
| :--- | :--- |
| **`GamifiedQuizApp.postman_environment.json`** | Environment variables (`baseUrl`, `accessToken`, `refreshToken`, `otp`, `sessionId`, `currentQuestion`, `topic`, `wsUrl`). |
| **`MCQ_Game_API.postman_collection.json`** | **[NEW]** Dedicated collection for Solo Player MCQ Game: Questions, Session lifecycle, Scoring with Combos & Speed multipliers, 50:50 power-up, and WebSocket Sudden Death engine. |
| **`GamifiedQuizApp_Full.postman_collection.json`** | Unified master collection containing all System, Auth, Profile, and MCQ Game endpoints. |
| **`Auth.postman_collection.json`** | Dedicated collection for Authentication, Registration, 2FA OTP, Sessions, and Passwords. |
| **`Profile.postman_collection.json`** | Dedicated collection for Client Profile, Gamification Stats (Gems, Coins, Level, XP, Weekly Rank), and 7-Day Streaks. |

---

## 🚀 How to Import into Postman

1. Open Postman.
2. Click **Import** (top left).
3. Drag & drop all `.json` files from this `Postman/` directory.
4. In the top-right environment dropdown, select **"Gamified Quiz App (Local Docker)"**.

---

## ⚡ Automated Testing & Authentication Flow

### 1. Authentication
1. **Direct Login (No OTP)**:
   - Run `POST /auth/login` with email `player@example.com` and password `secretpassword123`.
   - **The Postman test script automatically extracts `accessToken` and `refreshToken` and saves them directly into your Postman environment.**
2. **Subsequent Calls**:
   - All authenticated requests automatically inherit the `Bearer {{accessToken}}` header.

---

### 2. Solo Player MCQ Game Flow

The requests in **`MCQ_Game_API.postman_collection.json`** are numbered sequentially for easy execution:

1. **`1. Fetch Questions by Topic`** (`GET /api/v1/topics/{{topic}}/questions?limit=10`):
   - Returns 10 randomized multiple choice questions from the 100-question accounting database pool.
   - Formatted with standardized keys: `"question": "1"`, `"option": "a"`, etc.

2. **`2. Create Quiz Session`** (`POST /api/v1/quiz/sessions/create`):
   - Initializes a new solo quiz session.
   - **The Postman test script automatically saves `sessionId` and `currentQuestion` into the environment.**

3. **`3. Apply 50:50 Power-Up`** (`POST /api/v1/quiz/power-ups/fifty-fifty`):
   - Uses `{{sessionId}}` and `{{currentQuestion}}`.
   - Returns exactly 2 incorrect options to hide from the client UI.

4. **`4. Evaluate Answer (Submit Option)`** (`POST /api/v1/quiz/answers/evaluate`):
   - Submits the player's choice (`"option": "a"`, `"b"`, `"c"`, or `"d"`).
   - Server calculates speed bonus (up to 1.5x) and combo multiplier (up to 2.0x).
   - Returns updated total score, coins earned, and combo streak.

5. **`5. Evaluate Answer (Skip Question)`** (`POST /api/v1/quiz/answers/evaluate` with `"option": "skip"`):
   - Evaluates a skipped question without scoring penalties.

6. **`6. Complete Session & Claim Rewards`** (`POST /api/v1/quiz/sessions/complete`):
   - Finalizes the session atomically in PostgreSQL.
   - Awards coins, XP, and gems to `client_profiles` and logs entries in the append-only `wallet_ledger`.
   - Updates player's 7-day daily streak.

7. **`7. Abandon Session`** (`POST /api/v1/quiz/sessions/abandon`):
   - Abandons an active session without rewards.

8. **`8. Game History`** (`GET /api/v1/quiz/history?limit=10&offset=0`):
   - Retrieves paginated list of all past quiz runs with final scores and dates.

9. **`9. Sudden Death Live Game (WebSocket)`**:
   - Connects to `{{wsUrl}}/ws/game?session_id={{sessionId}}&token={{accessToken}}`.
   - Native WebSocket connection with real-time 15-second per-question timer, server-authoritative grading, and automatic question progression.
