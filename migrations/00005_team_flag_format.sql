-- Per-team flags (kind 'per-team') are derived from the server secret, the
-- challenge and the team, so they can be shown again without storing them.
-- flags.format is the template they were made with: a prefix, %s for the
-- random part, and a suffix (for example flag{%s}). NULL for other kinds.

-- +goose Up
ALTER TABLE flags ADD COLUMN format TEXT;

-- +goose Down
ALTER TABLE flags DROP COLUMN format;
