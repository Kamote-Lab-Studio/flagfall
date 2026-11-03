-- Theme preview is per admin session: the theme the admin is previewing on
-- the player site, or NULL. See docs/dev/admin-api.md (themes).

-- +goose Up
ALTER TABLE sessions ADD COLUMN preview_theme TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN preview_theme;
