# [MERGED INTO MASTER PLAN]

> [!NOTE]
> This preliminary plan has been merged into the single authoritative master architecture document:
> **[docs/ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md)**

Please refer to [ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md) for the complete, unified blueprint covering:
1. **Clean Table-Per-Game Catalog**: `mcq_questions`, `sort_it_out_questions`, `match_the_following_questions`, `match_pairs`.
2. **Pure Ephemeral Redis Sessions**: Eliminating DB session storage; sessions live purely in RAM with sliding 5-minute TTL and 0 DB writes.
3. **Unified Player Audit Trail**: `user_question_history` with `game_mode`.
4. **PostgreSQL Driver & Index Tuning**: `PrepareStmt: true`, `SkipDefaultTransaction: true`, connection pool tuning, and O(1) indexed code sampling (eliminating `ORDER BY RANDOM()`).
5. **6-Domain Redis Caching**: Ephemeral sessions, exclusion locks, real-time weekly leaderboard ZSET, topic catalog, and profile cache with `sync.Map` fallback.
