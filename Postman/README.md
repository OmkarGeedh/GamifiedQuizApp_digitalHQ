# 📬 Postman Collections & Environment Guide

This folder contains Postman collections and environment files for the **Gamified Quiz App API**.

---

## 📁 Files Included

| File | Description |
| :--- | :--- |
| **`GamifiedQuizApp.postman_environment.json`** | Environment variables (`baseUrl`, `accessToken`, `refreshToken`, `otp`, `resetToken`). |
| **`Auth.postman_collection.json`** | Dedicated collection for Authentication, Registration, 2FA OTP, Sessions, and Passwords. |
| **`Profile.postman_collection.json`** | Dedicated collection for Profile, Gamification Stats (Gems, Coins, Level, XP, Weekly Rank), and 7-Day Streaks. |
| **`GamifiedQuizApp_Full.postman_collection.json`** | Unified master collection containing all System, Auth, and Profile endpoints. |

---

## 🚀 How to Import into Postman

1. Open Postman.
2. Click **Import** (top left).
3. Drag & drop all `.json` files from this `postman/` directory.
4. In the top-right environment dropdown, select **"Gamified Quiz App (Local Docker)"**.

---

## ⚡ Automated Authentication Flow

1. **Trigger OTP**:
   - Run `POST /auth/login/otp` with email and password.
   - The OTP is logged in the server console (`make docker-logs`).
2. **Verify OTP**:
   - Run `POST /auth/login/verify-otp` with the OTP.
   - **Test Script automatically extracts and saves `accessToken` and `refreshToken` into your Postman environment.**
3. **Run Authenticated Requests**:
   - All subsequent requests (like `GET /profile`, `PUT /profile`, `GET /session`) automatically attach `Authorization: Bearer {{accessToken}}`.
4. **Token Refresh**:
   - Running `POST /auth/refresh` automatically updates your `accessToken` environment variable.
