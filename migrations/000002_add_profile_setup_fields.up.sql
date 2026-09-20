-- Migration: 000002_add_profile_setup_fields.up.sql
-- Description: Add avatar_id, class, board, subjects, and is_onboarded columns to client_profiles

ALTER TABLE IF EXISTS client_profiles
    ADD COLUMN IF NOT EXISTS avatar_id VARCHAR(50) NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS class VARCHAR(50),
    ADD COLUMN IF NOT EXISTS board VARCHAR(100),
    ADD COLUMN IF NOT EXISTS subjects TEXT DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS is_onboarded BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_client_profiles_onboarded ON client_profiles(is_onboarded);
