# Deployment

Flagfall is one binary (or one container image) with the themes and admin
panel built in. It needs a database: SQLite in the data folder by default,
or PostgreSQL for bigger events.

## How big a server

Measured with a load test of simulated players:

| Event | Server | Database |
| --- | --- | --- |
| Up to 100 players | 1 CPU, 1 GB | SQLite (default) |
| Up to 1,000 players, 300 teams | 2 CPU, 4 GB in total | PostgreSQL, on the same machine |

At 1,000 players the server answered 95% of requests within 6 ms and used
less than 200 MiB of memory. Twice that many players overloaded the
database at 0.75 CPU, so for bigger events give PostgreSQL more CPU.

## A public server with HTTPS (Docker Compose)

`deploy/docker-compose.yml` runs Flagfall, PostgreSQL and
[Caddy](https://caddyserver.com), which gets and renews the HTTPS
certificate by itself.

1. Point your domain's DNS (an `A`/`AAAA` record) at the server, and open
   ports 80 and 443.
2. On the server, in this folder:

       cp .env.example .env    # set FLAGFALL_DOMAIN and POSTGRES_PASSWORD
       docker compose up -d

3. Open `https://<your domain>/admin` and finish setup with the code from
   the log (the `setup_code` field):

       docker compose logs flagfall

The stack's project name is `flagfall-server`, so it never shares volumes
with the development `compose.yaml` at the repository root.

- **Data:** the database lives in the `flagfall-pg` volume, and the secret
  key and uploads in `flagfall-data`. Back up both, or use **Data → Backup**
  in the admin panel. Losing `flagfall-data` loses the secret key, which
  signs sessions and hashes flags.
- **Upgrades:** build the new version (`docker build -t flagfall:local ..` from an
  updated checkout, with `FLAGFALL_IMAGE=flagfall:local` in `.env`), then `docker compose up -d`.
  A version that changes the database backs it up to `/data/backups` first,
  and won't start while the event is running unless you confirm it
  (`docker compose run --rm flagfall migrate --during-event`). See
  [docs/config.md](../docs/config.md#upgrades).
- **Certificate notices:** add `email you@example.org` in a global block at
  the top of the `Caddyfile`.
- Flagfall isn't published on a port; Caddy is the only way in, which is why
  `FLAGFALL_TRUST_PROXY` is on.

## One container

No images are published yet, so build one from the repository root:

    docker build -t flagfall .
    docker run -d -p 8080:8080 -v flagfall-data:/data \
      -e FLAGFALL_PUBLIC_URL=https://ctf.example.org \
      flagfall

This uses SQLite in `/data`. Put a TLS proxy in front, and set
`FLAGFALL_TRUST_PROXY=true` only when that proxy is the sole way in.
To build for both `linux/amd64` and `linux/arm64`:

    docker buildx build --platform linux/amd64,linux/arm64 -t flagfall .

## A binary

Build it from source (Go 1.26+, Node 22+, pnpm 9):

    pnpm install && make build     # or: pnpm -r build, then go build -o bin/flagfall ./cmd/flagfall
    FLAGFALL_PUBLIC_URL=https://ctf.example.org ./bin/flagfall serve

`.goreleaser.yaml` builds binaries for Linux, macOS and Windows on amd64
and arm64: `goreleaser release --snapshot --clean`.

## Local development (Docker)

The repository root has a `compose.yaml` that builds the image and runs it
with PostgreSQL and Mailpit:

    docker compose up --build -d

Then open http://localhost:8090 (set `FLAGFALL_PORT` to change it). The
port is bound to 127.0.0.1 only. `FLAGFALL_SEED_SAMPLE` defaults to `true`
there, so the first start loads the sample event and its demo logins.
Mailpit catches every email at http://localhost:8026: in the admin panel's
Settings, use mail server `mailpit`, port `1025`, security "None".

## Still to come

A Helm chart (with Redis for several replicas) and cloud templates.
