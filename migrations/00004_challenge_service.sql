-- Challenge containers (the `service:` block in challenge.yml).
--
-- challenges.service holds the checked service spec as JSON, or NULL when
-- the challenge has none. challenge_services holds what the organizer asked
-- for (desired: running or stopped) and what the runner last saw.

-- +goose Up
ALTER TABLE challenges ADD COLUMN service TEXT;
ALTER TABLE challenge_services ADD COLUMN desired TEXT NOT NULL DEFAULT 'stopped';
ALTER TABLE challenge_services ADD COLUMN message TEXT NOT NULL DEFAULT '';
ALTER TABLE challenge_services ADD COLUMN host_port INTEGER;

-- +goose Down
ALTER TABLE challenge_services DROP COLUMN host_port;
ALTER TABLE challenge_services DROP COLUMN message;
ALTER TABLE challenge_services DROP COLUMN desired;
ALTER TABLE challenges DROP COLUMN service;
