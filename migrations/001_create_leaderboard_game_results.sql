-- Migration: 001_create_leaderboard_game_results.sql
-- Description: Creates the leaderboard_game_results table with composite indexes for fast leaderboard filtering and ranking.

CREATE TABLE IF NOT EXISTS leaderboard_game_results (
    id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(64) NOT NULL,
    client_id INTEGER NOT NULL,
    topic_id VARCHAR(64) NOT NULL,
    game_mode VARCHAR(32) NOT NULL,
    final_score INTEGER NOT NULL DEFAULT 0,
    completed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_lgr_session ON leaderboard_game_results (session_id);
CREATE INDEX IF NOT EXISTS idx_lgr_completed ON leaderboard_game_results (completed_at);
CREATE INDEX IF NOT EXISTS idx_lgr_client ON leaderboard_game_results (client_id);
CREATE INDEX IF NOT EXISTS idx_lgr_topic ON leaderboard_game_results (topic_id);
CREATE INDEX IF NOT EXISTS idx_lgr_mode ON leaderboard_game_results (game_mode);
CREATE INDEX IF NOT EXISTS idx_lgr_query ON leaderboard_game_results (client_id, topic_id, game_mode, completed_at);
