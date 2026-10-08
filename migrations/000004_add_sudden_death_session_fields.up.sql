-- Persist authoritative Sudden Death completion and streak state.
ALTER TABLE game_sessions
    ADD COLUMN IF NOT EXISTS streak_milestones INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS skipped_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS end_reason VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS completed_successfully BOOLEAN NOT NULL DEFAULT FALSE;
