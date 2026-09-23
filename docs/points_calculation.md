# Points, Scoring & Rewards Specification

## 1. Overview & Principles

The scoring engine in `GamifiedQuizApp_digitalHQ` provides a server-authoritative, deterministic gamification algorithm. It rewards players based on knowledge (correctness), depth (question difficulty), response velocity (speed factor), and consecutive accuracy (combo streaks).

Scoring eliminates arbitrary or random multipliers in favor of a formula where every awarded point is verifiable by both the player and the game engine.

---

## 2. Per-Question Point Calculation (`CalculatePoints`)

When a player submits a correct answer to an MCQ question, the awarded points are computed via `CalculatePoints`:

$$\text{PointsEarned} = \text{round}\Big(\text{basePoints} \times \text{difficultyMultiplier} \times \text{speedFactor} \times \text{comboMultiplier}\Big)$$

Incorrect answers or skipped questions always award **0 points**.

### 2.1 Variables & Definitions

| Parameter | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `basePoints` | `int` | 10 | Base question points (typically 10 for Level 1, 15 for Level 2, 20 for Level 3). |
| `difficulty` | `int` | 1 | Question difficulty level (1 = Easy, 2 = Medium, 3 = Hard). |
| `timeTakenMs` | `int` | 3000 | Milliseconds elapsed between displaying the question and receiving the answer. |
| `comboStreak` | `int` | 0 | Number of consecutive correct answers within the active session. |

---

### 2.2 Multipliers & Thresholds

#### A. Difficulty Multiplier (`difficultyMultiplier`)
| Question Difficulty | Multiplier Value | Description |
| :---: | :---: | :--- |
| **1 (Easy)** | **$1.0\times$** | Standard base multiplier |
| **2 (Medium)** | **$1.5\times$** | $50\%$ bonus for intermediate problems |
| **3 (Hard)** | **$2.0\times$** | $100\%$ bonus for advanced problems |

#### B. Speed Factor (`speedFactor`)
The default question time limit is **15 seconds** ($15,000\text{ ms}$).

| Ratio ($\frac{\text{timeTakenMs}}{\text{timeLimitMs}}$) | Elapsed Time | Multiplier Value | Tier |
| :---: | :---: | :---: | :--- |
| **$< 0.50$** | $< 7.5\text{ s}$ | **$1.50\times$** | **Lightning Speed** ($50\%$ speed bonus) |
| **$< 0.75$** | $7.5\text{ s} - 11.25\text{ s}$ | **$1.25\times$** | **Fast Speed** ($25\%$ speed bonus) |
| **$\ge 0.75$** | $11.25\text{ s} - 15.0\text{ s}$ | **$1.00\times$** | **Standard** (No bonus) |

*Note: Answers exceeding $15.0\text{ s}$ trigger a timeout and award 0 points.*

#### C. Combo Streak Multiplier (`comboMultiplier`)
Streaks increment on consecutive correct answers and reset to 0 immediately upon any wrong or skipped answer.

| Consecutive Correct Answers | Multiplier Value | Tier |
| :---: | :---: | :--- |
| **$\ge 7$ in a row** | **$2.0\times$** | **Legendary Combo** ($100\%$ bonus) |
| **$\ge 5$ in a row** | **$1.5\times$** | **Hot Streak** ($50\%$ bonus) |
| **$\ge 3$ in a row** | **$1.2\times$** | **Warm Combo** ($20\%$ bonus) |
| **$< 3$ in a row** | **$1.0\times$** | **Base** (No combo bonus) |

---

### 2.3 Worked Examples

#### Example 1: Easy Question, Lightning Speed, Beginning of Quiz
- `basePoints`: 10 (Difficulty: 1)
- `timeTakenMs`: 2,400 ms ($< 7.5\text{s} \implies 1.5\times$)
- `comboStreak`: 1 ($< 3 \implies 1.0\times$)
$$\text{Points} = \text{round}(10 \times 1.0 \times 1.5 \times 1.0) = \mathbf{15\text{ points}}$$

#### Example 2: Medium Question, Fast Speed, Active Streak of 4
- `basePoints`: 15 (Difficulty: 2)
- `timeTakenMs`: 9,000 ms ($< 11.25\text{s} \implies 1.25\times$)
- `comboStreak`: 4 ($\ge 3 \implies 1.2\times$)
$$\text{Points} = \text{round}(15 \times 1.5 \times 1.25 \times 1.2) = \text{round}(33.75) = \mathbf{34\text{ points}}$$

#### Example 3: Hard Question, Lightning Speed, Legendary Streak of 7
- `basePoints`: 20 (Difficulty: 3)
- `timeTakenMs`: 3,500 ms ($< 7.5\text{s} \implies 1.5\times$)
- `comboStreak`: 7 ($\ge 7 \implies 2.0\times$)
$$\text{Points} = \text{round}(20 \times 2.0 \times 1.5 \times 2.0) = \mathbf{120\text{ points}}$$

---

## 3. Session Maximum Score (`maxScore`)

### 3.1 The Problem It Solves
Previously, a session's `maxScore` was hardcoded to `totalQuestions * 10` ($100$ points for 10 questions) or the unmultiplied sum of base points. When a player earned bonuses through `CalculatePoints`, their score could reach 90 points on just 3 questions, displaying **90 / 100** (giving the false impression that 3 questions accounted for $90\%$ of the quiz). Furthermore, once the earned score exceeded 100, the backend clamped `maxScore = session.Score`, displaying **125 / 125** ($100\%$) even for imperfect games.

### 3.2 The Harmonized Formula
`maxScore` represents the **theoretical maximum score** a player could possibly achieve in that specific quiz session under the `CalculatePoints` algorithm:

$$\text{maxScore} = \sum_{i=1}^{N} \text{CalculatePoints}\big(\text{sq.Points}_i,\; \text{sq.Difficulty}_i,\; 1000\text{ ms},\; i\big)$$

Where:
- $N$ is the total number of questions in the session.
- Each question $i$ is calculated assuming **perfect velocity** ($1000\text{ ms} \implies 1.5\times$ speed bonus).
- Each question $i$ is calculated assuming an **unbroken streak** from question 1 through $N$ (streak = $i$).

#### Standard 10-Question Easy Quiz Benchmark (Base 10, Difficulty 1):
| Question ($i$) | Streak | Base | Difficulty | Speed | Combo | Max Question Points | Running Total |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Q1** | 1 | 10 | $1.0\times$ | $1.5\times$ | $1.0\times$ | 15 | 15 |
| **Q2** | 2 | 10 | $1.0\times$ | $1.5\times$ | $1.0\times$ | 15 | 30 |
| **Q3** | 3 | 10 | $1.0\times$ | $1.5\times$ | $1.2\times$ | 18 | 48 |
| **Q4** | 4 | 10 | $1.0\times$ | $1.5\times$ | $1.2\times$ | 18 | 66 |
| **Q5** | 5 | 10 | $1.0\times$ | $1.5\times$ | $1.5\times$ | 23 | 89 |
| **Q6** | 6 | 10 | $1.0\times$ | $1.5\times$ | $1.5\times$ | 23 | 112 |
| **Q7** | 7 | 10 | $1.0\times$ | $1.5\times$ | $2.0\times$ | 30 | 142 |
| **Q8** | 8 | 10 | $1.0\times$ | $1.5\times$ | $2.0\times$ | 30 | 172 |
| **Q9** | 9 | 10 | $1.0\times$ | $1.5\times$ | $2.0\times$ | 30 | 202 |
| **Q10** | 10 | 10 | $1.0\times$ | $1.5\times$ | $2.0\times$ | 30 | **232** |

- **Total Achievable Score**: **232 points** (for all Easy questions) or up to **$350+$ points** (for mixed Medium/Hard questions).
- Answering 3 questions correctly fast with a streak yields **$48$ points**, displaying **48 / 232** (accurately reflecting $\approx 20.7\%$ of maximum possible points).
- Answering all 10 questions perfectly yields **232 / 232** ($100\%$).
- Earned score can never exceed `maxScore`.

---

## 4. Derived Economy & Rewards

Rewards are calculated upon session completion via `CalculateGameRewards`:

### 4.1 Coins (`coinsAwarded`)
Coins serve as in-game currency for power-ups (e.g., 50:50, skip).
$$\text{Performance Coins} = \left\lceil \frac{\text{session.Score}}{5.0} \right\rceil$$
$$\text{Total Coins} = \text{Performance Coins} + 5\text{ (Completion Bonus)} + \Big((\text{newLevel} - \text{oldLevel}) \times 50\Big)$$

### 4.2 Experience Points (`xpAwarded`)
XP contributes directly to account level progression.
$$\text{Performance XP} = \left\lceil \frac{\text{session.Score}}{2.0} \right\rceil$$
$$\text{Total XP} = \text{Performance XP} + 10\text{ (Completion Bonus)} + \Big((\text{newLevel} - \text{oldLevel}) \times 25\Big)$$

### 4.3 Gems (`gemsAwarded`)
Gems are premium currency awarded for high accuracy:
$$\text{Accuracy} = \frac{\text{session.CorrectCount}}{\text{session.TotalQuestions}}$$
- **Accuracy $= 100\%$ (Perfect Game)**: **3 Gems**
- **Accuracy $\ge 80\%$ (Mastery)**: **1 Gem**
- **Accuracy $< 80\%$**: **0 Gems**

### 4.4 Level Progression
Player level is derived from accumulated experience:
$$\text{Level} = \left\lfloor \sqrt{\frac{\text{Total Experience}}{100}} \right\rfloor + 1$$
Experience required to reach level $L$:
$$\text{ExpRequired}(L) = (L - 1)^2 \times 100$$

---

## 5. Inactivity Time-To-Live (5-Minute TTL)

To prevent abandoned or stalled sessions from locking resources or distorting leaderboards, sessions enforce a strict **5-minute inactivity TTL**:

- **Duration**: `SessionInactivityTTL = 5 * time.Minute`.
- **Activity Definition**: An activity event is registered whenever an answer is submitted (`EvaluateAnswer`), a power-up is used (`ApplyFiftyFifty`), or a session is completed (`CompleteSession`).
- **Inline Interception**: If `time.Since(session.UpdatedAt) > 5 * time.Minute`, the session is transitioned to `abandoned`, `ended_at` is stamped, Redis is cleared, and the operation is rejected with `HTTP 409 Conflict`.
- **Auto-Recovery on Create**: When a player starts a new quiz while holding a stale session ($> 5\text{ min}$ old), `CreateSession` auto-abandons the old session and creates the fresh session seamlessly.
- **Background Sweeper**: `StartSessionTTLSweeper` runs periodically every 1 minute to sweep PostgreSQL and transition any unclosed sessions inactive for $> 5\text{ min}$ to `status = 'abandoned'`.
- **Redis TTL**: Session keys (`quiz:session:<session_id>`) maintain a 5-minute TTL refreshed on every user interaction.

---

## 6. Implementation References

- [game_service.go](file:///Users/omkargeedh/Developer/Gamified%20Quiz%20App/GamifiedQuizApp_digitalHQ/internal/services/game_service.go): Contains `CalculatePoints`, `CalculateCoins`, `CalculateGameRewards`, `CalculateNewLevel`, `CheckAndAbandonIfExpired`, and `StartSessionTTLSweeper`.
- [session.go](file:///Users/omkargeedh/Developer/Gamified%20Quiz%20App/GamifiedQuizApp_digitalHQ/internal/ws/session.go): Live WebSocket session loop implementing `CalculatePoints` in real time.
- [game_repo.go](file:///Users/omkargeedh/Developer/Gamified%20Quiz%20App/GamifiedQuizApp_digitalHQ/internal/repo/game_repo.go): `AbandonInactiveSessions` query and atomic ledger finalization.
- [quiz_remote_datasource.dart](file:///Users/omkargeedh/Developer/Gamified%20Quiz%20App/Gamified-learning/lib/features/quiz/data/datasources/remote/quiz_remote_datasource.dart): Flutter remote datasource capturing dynamic `timeTakenMs` and parsing authoritative scoring.
