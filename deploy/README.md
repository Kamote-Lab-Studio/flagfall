# Deployment

Flagfall is one binary (or one container image) with the themes and admin
panel built in. It needs a database: SQLite in the data folder by default,
or PostgreSQL for bigger events.

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
- **Upgrades:** set `FLAGFALL_IMAGE` to a release tag (for example
  `ghcr.io/flagfall/flagfall:1.0.0`), then `docker compose pull && docker compose up -d`.
  A version that changes the database backs it up to `/data/backups` first,
  and won't start while the event is running unless you confirm it
  (`docker compose run --rm flagfall migrate --during-event`). See
  [docs/dev/config.md](../docs/dev/config.md#upgrades).
- **Certificate notices:** add `email you@example.org` in a global block at
  the top of the `Caddyfile`.
- Flagfall isn't published on a port; Caddy is the only way in, which is why
  `FLAGFALL_TRUST_PROXY` is on.

## One container

    docker run -d -p 8080:8080 -v flagfall-data:/data \
      -e FLAGFALL_PUBLIC_URL=https://ctf.example.org \
      ghcr.io/flagfall/flagfall:latest

This uses SQLite in `/data`. Put a TLS proxy in front, and set
`FLAGFALL_TRUST_PROXY=true` only when that proxy is the sole way in.
Images are published for `linux/amd64` and `linux/arm64` and signed with
cosign (keyless).

## A release binary

Download the archive for your system from the GitHub release, check it
against `checksums.txt` (signed: `checksums.txt.sigstore.json`), and run:

    FLAGFALL_PUBLIC_URL=https://ctf.example.org ./flagfall serve

Binaries are built for Linux, macOS and Windows on amd64 and arm64, each
with an SPDX SBOM.

## Local development (Docker)

The repository root has a `compose.yaml` that builds the image and runs it
with PostgreSQL and Mailpit:

    docker compose up --build -d

Then open http://localhost:8090 (set `FLAGFALL_PORT` to change it). The
port is bound to 127.0.0.1 only. `FLAGFALL_SEED_SAMPLE` defaults to `true`
there, so the first start loads the sample event and its demo logins.
Mailpit catches every email at http://localhost:8026: in the admin panel's
Settings, use mail server `mailpit`, port `1025`, security "None".

## Releasing (maintainers)

Push a tag like `v1.0.0`. `.github/workflows/release.yml` then:

- builds the six binaries with GoReleaser (`.goreleaser.yaml`), with
  checksums, SBOMs and a keyless cosign signature, into a **draft** release;
- builds and pushes the multi-arch image to `ghcr.io/flagfall/flagfall`
  (`1.0.0`, `1.0` and `latest`; tags with a `-`, like `v1.0.0-rc.1`, don't
  move `latest`) and signs it.

To build the image for both architectures locally:

    docker buildx build --platform linux/amd64,linux/arm64 -t flagfall .

## Still to come

A Helm chart (with Redis for several replicas) and cloud templates.
