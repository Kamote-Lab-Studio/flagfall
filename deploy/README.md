# Deployment

## Docker (local)

The repository root has a `Dockerfile` and a `compose.yaml` that runs
Flagfall with PostgreSQL:

    docker compose up --build -d

Then open http://localhost:8090 (set `FLAGFALL_PORT` to change it). The
port is bound to 127.0.0.1 only.

`FLAGFALL_SEED_SAMPLE` defaults to `true` in the compose file, so the
first start loads the sample event and its public demo logins (shown on
both sign-in pages). For a real event, start with it off and empty volumes:

    FLAGFALL_SEED_SAMPLE=false docker compose up --build -d   # after: docker compose down -v

and finish setup at `/admin` with the code printed in
`docker compose logs flagfall`.

The image runs as a non-root user on a distroless base, keeps the secret
key and uploads in the `/data` volume, and checks itself with
`flagfall healthcheck`.

## Later (Phase 3)

Multi-arch images on ghcr.io, a production compose file with Caddy and
automatic HTTPS, a Helm chart and cloud templates.
