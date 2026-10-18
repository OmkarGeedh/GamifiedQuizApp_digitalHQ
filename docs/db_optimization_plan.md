# [MERGED INTO MASTER PLAN]

> [!NOTE]
> This database optimization plan has been unified and merged into the single authoritative master architecture document:
> **[docs/ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md)**

Please refer to [ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md) for the complete, unified blueprint covering:
1. **PostgreSQL Driver & Pool Optimization**: `PrepareStmt: true`, `SkipDefaultTransaction: true`, connection pool sizing (`MaxOpen: 50`, `MaxIdle: 25`).
2. **O(1) Indexed Question Sampling**: Eliminating `ORDER BY RANDOM()` via index-only scans and Go memory/Redis sampling.
3. **Multi-Row Batch Writes**: `CreateInBatches` for answers and ledger records in a single atomic transaction.
4. **Clean Table-Per-Game Schema**: `mcq_questions`, `sort_it_out_questions`, `match_the_following_questions`, `match_pairs`.
5. **Pure Ephemeral Redis Sessions**: Eliminating DB session storage; sessions live purely in RAM with sliding 5m TTL and 0 DB writes during play.
6. **Multi-Tier 6-Domain Redis Caching**: Real-time weekly leaderboard ZSET, profile cache, topic catalog with SingleFlight stampede protection, and `sync.Map` fallback.
