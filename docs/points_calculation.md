# MCQ scoring and rewards

## Authority and scope

The backend is the source of truth for MCQ answer evaluation, points, XP, and
coins. Flutter submits an answer and displays the completion response; it does
not own the production scoring formula.

These rules apply only to MCQ. Other game modes keep their own behavior.

## Quiz points

| Outcome | Points |
| --- | ---: |
| Correct answer | 10 |
| Wrong answer | 0 |
| Skipped answer | 0 |

Difficulty, answer time, combo streak, and power-up usage do not modify MCQ
points.

The maximum score is calculated from the actual question count:

```text
max_score = total_questions * 10
final_score = correct_count * 10
```

## XP

```text
xp_awarded = 10 + (correct_count * 5) + perfect_bonus
```

- Quiz completion: 10 XP
- Each correct answer: 5 XP
- Perfect score: 15 XP
- Wrong and skipped answers: 0 answer XP

The perfect bonus applies only when `total_questions > 0` and
`correct_count == total_questions`.

## Coins

```text
coins_awarded = 5 + (correct_count * 2) + perfect_bonus
```

- Quiz completion: 5 coins
- Each correct answer: 2 coins
- Perfect score: 10 coins
- Wrong and skipped answers: 0 answer coins

The perfect bonus applies only when `total_questions > 0` and
`correct_count == total_questions`.

Power-up usage does not reduce the MCQ score, XP, or coin reward. Existing
power-up purchase and wallet debit behavior is unchanged.

## Completion response

The MCQ completion response includes backend-authoritative totals and
breakdowns:

```json
{
  "final_score": 50,
  "max_score": 100,
  "correct_count": 5,
  "total_questions": 10,
  "accuracy_percentage": 50,
  "xp_awarded": 35,
  "coins_awarded": 15,
  "score_breakdown": {
    "correct_answer_points": 50
  },
  "xp_breakdown": {
    "completion_xp": 10,
    "correct_answer_xp": 25,
    "perfect_bonus_xp": 0
  },
  "coin_breakdown": {
    "completion_coins": 5,
    "correct_answer_coins": 10,
    "perfect_bonus_coins": 0
  }
}
```

Zero-value breakdown entries may be returned by the API; clients should hide
them from the visible result breakdown.

## Level-up rewards

The existing level-up reward remains a separate economy event. It is returned
as `level_up_reward` and is not included in `xp_awarded`, `coins_awarded`, or
their MCQ breakdowns. The wallet transaction still receives both the base MCQ
reward and the separate level-up reward.

## Examples

| Correct | Total | Score | XP | Coins |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 10 | 0 / 100 | 10 | 5 |
| 1 | 10 | 10 / 100 | 15 | 7 |
| 5 | 10 | 50 / 100 | 35 | 15 |
| 9 | 10 | 90 / 100 | 55 | 23 |
| 10 | 10 | 100 / 100 | 75 | 35 |
