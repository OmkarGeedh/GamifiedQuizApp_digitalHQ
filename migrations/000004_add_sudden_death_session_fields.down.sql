ALTER TABLE game_sessions
    DROP COLUMN IF EXISTS completed_successfully,
    DROP COLUMN IF EXISTS end_reason,
    DROP COLUMN IF EXISTS skipped_count,
    DROP COLUMN IF EXISTS streak_milestones;
