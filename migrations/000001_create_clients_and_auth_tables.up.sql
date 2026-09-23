-- Migration: 000001_create_clients_and_auth_tables.up.sql
-- Description: Create clients table with explicit unique constraints and indexes for username, email, and phone

CREATE TABLE IF NOT EXISTS clients (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    phone VARCHAR(20),
    password VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    refresh_token VARCHAR(512),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Explicit unique constraints
    CONSTRAINT uq_clients_username UNIQUE (username),
    CONSTRAINT uq_clients_email UNIQUE (email),
    CONSTRAINT uq_clients_phone UNIQUE (phone)
);

-- Performance and lookup indexes
CREATE INDEX IF NOT EXISTS idx_clients_status ON clients(status);
CREATE INDEX IF NOT EXISTS idx_clients_created_at ON clients(created_at);
