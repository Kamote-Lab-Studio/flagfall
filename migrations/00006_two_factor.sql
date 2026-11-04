-- Two-factor sign-in (TOTP), optional, meant for staff.
--
-- users.totp_secret (already in the schema) holds the sealed secret once
-- two-factor is on. totp_pending holds a sealed secret during setup, until
-- the first code confirms it. totp_last_step is the last time step a code was
-- used for, so a code works once. totp_recovery is a JSON array of hashed
-- one-time recovery codes.

-- +goose Up
ALTER TABLE users ADD COLUMN totp_pending TEXT;
ALTER TABLE users ADD COLUMN totp_last_step BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_recovery TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE users DROP COLUMN totp_recovery;
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_pending;
