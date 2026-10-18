# [MERGED INTO MASTER PLAN]

> [!NOTE]
> This caching plan has been unified and merged into the single authoritative master architecture document:
> **[docs/ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md)**

Please refer to [ultimate_db_redis_optimization_plan.md](../docs/ultimate_db_redis_optimization_plan.md) for the complete, unified blueprint covering:
1. **The 3-Tier Hierarchy**: Tier 1 Go in-memory RAM (< 1µs), Tier 2 Distributed Redis 7 (< 1ms), Tier 3 PostgreSQL 16 (ACID).
2. **The 6 Caching Domains**:
   - Ephemeral Active Game Sessions (`quiz:session:cache:*`, sliding 5m TTL).
   - Active Session Exclusion Locks (`quiz:client:active:*`, 5m TTL).
   - Real-Time Weekly Leaderboards (`quiz:leaderboard:weekly`, Redis Sorted Set `ZSET`).
   - Question Sets & Topic Catalogs (`quiz:catalog:*` with SingleFlight stampede protection).
   - User Profiles & Gamification Stats (`quiz:profile:*`, 15m TTL).
   - Answer Deduplication & Concurrency Locks (`quiz:lock:answer:*`, 10s `SETNX`).
3. **Decoupled Fallback Guarantee**: `sync.Map` in-memory fallback when `config.RedisClient == nil` for offline test isolation.
4. **PostgreSQL Driver & Index Tuning**: `PrepareStmt: true`, `SkipDefaultTransaction: true`, connection pool tuning, and O(1) indexed code sampling.
5. **Clean Table-Per-Game Schema**: `mcq_questions`, `sort_it_out_questions`, `match_the_following_questions`, `match_pairs`.
