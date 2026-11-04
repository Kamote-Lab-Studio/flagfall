# Challenge containers

A challenge with a `service:` block in its `challenge.yml` can have one shared container that all teams connect to. Flagfall builds or pulls the image, starts the container with limits on its own network, publishes its port, and shows its health on the admin dashboard. Per-team instances (each team gets its own short-lived copy) are planned for 1.2; Kubernetes support for Phase 3.

```yaml
connection: "nc {{event_domain}} 31337"
service:
  image: ./Dockerfile     # or an image name, such as ghcr.io/me/pwn-notes:1.0
  port: 1337              # inside the container
  publish: 31337          # on the host; leave out for any free port
  cpu: 0.5                # default 0.5, max 4
  memory: 256Mi           # default 256Mi, 16Mi to 8Gi
  healthcheck: tcp        # or an HTTP path such as /health
  env:
    MODE: ctf
```

## Turning it on

Containers are off until Flagfall can reach the Docker API:

- **Binary:** `FLAGFALL_DOCKER_HOST=unix:///var/run/docker.sock flagfall serve`. The user running Flagfall needs access to the socket (on Linux, the `docker` group). On Windows, use Docker Desktop's "Expose daemon on tcp://localhost:2375" setting and `tcp://127.0.0.1:2375`; named pipes aren't supported.
- **Docker Compose:** `docker compose -f compose.yaml -f compose.services.yaml up -d`. The override mounts the socket, adds the socket's group (`DOCKER_GID`, 0 on Docker Desktop) and sets the health check address to `host.docker.internal`.

See [config.md](config.md#challenge-containers) for every setting.

**Security.** Whoever controls the Docker API controls the host, so giving it to Flagfall means a bug in Flagfall, or a stolen admin account, can take over the machine. Run events that use this on a machine dedicated to the event, with no other data on it, and keep admin accounts to people you'd trust with the host. Challenge containers themselves are limited (below), but they're still meant to be attacked: don't run them next to anything you care about.

## Running a service

Importing a challenge never starts anything. On the dashboard, the **Challenge containers** table lists every service; an admin clicks **Start**. Within a few seconds Flagfall:

1. Builds the image from the challenge folder (tagged `flagfall-chal/<slug>:<hash>`) or pulls the named image, if it isn't there yet.
2. Creates the network `flagfall-chal-<slug>`.
3. Creates and starts the container `flagfall-chal-<slug>`.
4. Checks every 15 seconds that it answers on the published port.

States: **stopped**, **starting** (building, pulling or waiting for its first answer, up to 30 seconds), **running**, **unhealthy** (up but not answering, or crash-looping) and **failed** (it couldn't be built or started, or exited more than 5 times; the message says why). Unhealthy and failed services also show as dashboard warnings.

**Restart** replaces the container with a fresh one from the same image. **Stop** removes the container and its network. Retired challenges are stopped. When a challenge is deleted, or loses its `service:` block, its container goes too.

Re-importing a challenge whose service changed (settings, or any file in the build folder) rebuilds the image and replaces the running container on the next pass. Players connected at that moment are dropped.

## Limits and isolation

Each container gets:

| Limit | Value |
| --- | --- |
| CPU | `cpu` (default half a core) |
| Memory | `memory`, with no swap on top |
| Processes | 256 (stops fork bombs) |
| Privileges | `no-new-privileges`; `NET_RAW`, `MKNOD`, `AUDIT_WRITE`, `SETFCAP` and `SYS_CHROOT` dropped |
| Restarts | Docker restarts it up to 5 times after a crash, then it's reported as failed |
| Logs | 2 files of 10 MB (`docker logs flagfall-chal-<slug>`) |
| Network | Its own bridge network, with container-to-container traffic off. It can't reach Flagfall, its database or other challenges by name. It can reach the internet and the host's published ports. |

## Build folders

With `image: ./Dockerfile`, the whole challenge folder except `challenge.yml` is the build context: `challenge.yml` holds the flags, so leaving it out means a `COPY . .` can't put them in the image. Put the flag the service needs in a file in the folder (or in `env`) on purpose. The folder is stored with the challenge (at most 50 MB, no symlinks), so admin `.zip` uploads, exports and backups include it. `.dockerignore` isn't applied.

## Connection details

Players see `connection` as written. With `publish`, write the port in it (`nc {{event_domain}} 31337`). Without `publish`, Docker picks a free port each time the container is created; the dashboard shows it, but you'd have to update `connection` by hand, so set `publish` for real events.
