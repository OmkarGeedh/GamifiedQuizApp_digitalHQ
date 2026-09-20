-- Migration: 000002_add_profile_setup_fields.down.sql
-- Description: Revert profile setup onboarding columns from client_profiles

DROP INDEX IF EXISTS idx_client_profiles_onboarded;

ALTER TABLE IF EXISTS client_profiles
    DROP COLUMN IF EXISTS avatar_id,
    DROP COLUMN IF EXISTS class,
    DROP COLUMN IF EXISTS board,
    DROP COLUMN IF EXISTS subjects,
    DROP COLUMN IF EXISTS is_onboarded;
