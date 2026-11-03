-- Images organizers upload for theme settings (logo, favicon). The bytes
-- live in blob storage under their SHA-256; this table says which blobs are
-- theme images, so only those are served publicly at /theme-assets/{sha256}.

-- +goose Up
CREATE TABLE theme_images (
  sha256        TEXT PRIMARY KEY,
  content_type  TEXT NOT NULL,
  size          BIGINT NOT NULL,
  uploaded_by   BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at    {{ts}} NOT NULL
);

-- +goose Down
DROP TABLE theme_images;
