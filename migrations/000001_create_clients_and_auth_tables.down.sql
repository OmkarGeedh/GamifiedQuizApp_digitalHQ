-- Migration: 000001_create_clients_and_auth_tables.down.sql
-- Description: Revert clients table creation

DROP INDEX IF EXISTS idx_clients_created_at;
DROP INDEX IF EXISTS idx_clients_status;
DROP TABLE IF EXISTS clients CASCADE;
