# Configuration

Almost everything is set in the admin panel and stored in the database. Only what's needed before the server can start comes from the environment or a `flagfall.toml` file.

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLAGFALL_ADDR` | `:8080` | Address to listen on |
| `FLAGFALL_PUBLIC_URL` | `http://localhost:8080` | URL players use; must be `http` or `https`. Used for cookies, links in emails and the CSRF origin check |
| `FLAGFALL_DATA_DIR` | `./data` | SQLite database, uploaded files and themes |
| `FLAGFALL_DATABASE_URL` | `sqlite://<data dir>/flagfall.db` | `sqlite://...` or `postgres://...` |
| `FLAGFALL_SECRET_KEY` | generated on first start, saved as `<data dir>/secret.key` | Signs sessions, hashes flags, encrypts 2FA secrets. Changing it signs everyone out and invalidates stored flag hashes |
| `FLAGFALL_TRUST_PROXY` | `false` | Read the client IP from `X-Forwarded-For`. Turn on only behind a reverse proxy, or players can fake IPs to dodge rate limits |
| `FLAGFALL_MIGRATE_DURING_EVENT` | `false` | Let an upgrade change the database while the event is running. Without it, a new version that changes the database refuses to start mid-event (see Upgrades below) |
| `FLAGFALL_CONFIG` | `./flagfall.toml` if present | Path to a TOML file with the same settings in lowercase (`addr`, `public_url`, `data_dir`, `database_url`, `secret_key`, `trust_proxy`, `migrate_during_event`) |
| `FLAGFALL_REDIS_URL` | unset *(Phase 3)* | Only when running several copies |

Order of precedence: environment variables, then `flagfall.toml`, then defaults.

## File storage

Challenge files are stored by SHA-256 in `<data dir>/files` unless a bucket is set. Any S3-compatible service works: AWS S3, Cloudflare R2, Backblaze B2, MinIO, Garage, and others.

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLAGFALL_S3_BUCKET` | unset (local disk) | Bucket name. Setting it switches storage to S3 |
| `FLAGFALL_S3_ENDPOINT` | `s3.amazonaws.com` | `host[:port]` or URL. `http://host:port` turns TLS off (a local MinIO only) |
| `FLAGFALL_S3_REGION` | unset | Needed by AWS for most regions; others ignore it or want `auto` |
| `FLAGFALL_S3_ACCESS_KEY`, `FLAGFALL_S3_SECRET_KEY` | unset | Both or neither. Neither means `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, `~/.aws/credentials`, then an instance or task role |
| `FLAGFALL_S3_PREFIX` | none | Folder within the bucket, e.g. `flagfall/`, to share a bucket |
| `FLAGFALL_S3_PATH_STYLE` | `false` | `endpoint/bucket/key` URLs instead of `bucket.endpoint/key`. MinIO usually needs it |
| `FLAGFALL_S3_REDIRECT` | `false` | Send players to the bucket with a signed link that works for 5 minutes, instead of passing downloads through the server. Saves the server's bandwidth on big files; players' browsers must be able to reach the endpoint |

In `flagfall.toml` these go in an `[s3]` table: `bucket`, `endpoint`, `region`, `access_key`, `secret_key`, `prefix`, `path_style`, `redirect`.

The server checks the bucket when it starts and refuses to start if it can't reach it. The key needs `GetObject`, `PutObject` and `ListBucket` (for the startup check) on the bucket; files are never deleted or overwritten, so it doesn't need `DeleteObject`. Keep the bucket private: players only get files through the server, or through signed links after the server has checked they may see the challenge.

Storage isn't switched in the admin panel, because changing it mid-event would break every download. To move existing files, copy the contents of `<data dir>/files` to the bucket (under the prefix, if set) before restarting with the new settings, or restore a backup after the switch: backups include the files and restores upload them to whichever storage is active.

## Challenge containers

Flagfall can start one shared container for each challenge with a `service:` block (see [challenge-services.md](challenge-services.md)). It's off until a Docker host is set.

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLAGFALL_DOCKER_HOST` | unset (off) | Docker API: `unix:///var/run/docker.sock` or `tcp://host:port`. Access to the Docker API is access to the whole host: only give it to Flagfall on a machine dedicated to the event |
| `FLAGFALL_SERVICES_BIND` | `0.0.0.0` | Host address that challenge ports are published on |
| `FLAGFALL_SERVICES_PROBE_HOST` | `127.0.0.1` | Where Flagfall reaches published ports for health checks. When Flagfall itself runs in a container, use `host.docker.internal` (see `compose.services.yaml`) |

In `flagfall.toml` these go in a `[services]` table: `docker_host`, `bind`, `probe_host`.

## Upgrades

On start (and with `flagfall migrate`), Flagfall brings the database schema up to date. When the new version changes an existing database, it first:

1. **Checks the event.** If the event has started and hasn't ended, it refuses to start and says so. Upgrade after the event, go back to the version you had, or confirm with `flagfall migrate --during-event` (or start once with `FLAGFALL_MIGRATE_DURING_EVENT=true`). An event with no start time doesn't count as running.
2. **Writes a backup** to `<data dir>/backups/before-upgrade-schema-<from>-to-<to>-<time>.zip`: the same format as `flagfall backup`, of the database before the change. If the backup fails, nothing is changed. To undo an upgrade, restore that file with the version you had (`flagfall restore --replace`).

A new, empty database is set up without either step. Old backups aren't deleted; remove them when you no longer need them.
