# Challenge format (`challenge.yml`)

Machine-readable version: [`schemas/challenge.schema.json`](schemas/challenge.schema.json). Authors can add `# yaml-language-server: $schema=https://raw.githubusercontent.com/Kamote-Lab-Studio/flagfall/main/docs/schemas/challenge.schema.json` at the top of the file to get autocomplete in VS Code.

## A challenge is a folder

```
challenges/
  cookie-jar/
    challenge.yml
    description.md
    hints/1.md
    dist/source.zip        files players download
    Dockerfile             optional: the challenge server
    solve/                 optional: author's solution; never imported
```

## Example

```yaml
format: 1
slug: cookie-jar
name: Cookie Jar
category: Web
difficulty: Easy
author: mira
value: 100                 # static, or use `scoring` (not both):
# scoring: {initial: 500, minimum: 50, decay: 100}
description: description.md
flags:
  - type: exact
    value: flag{c00k1es_ar3_n0t_s3cr3ts}
hints:
  - cost: 25
    content: hints/1.md
files:
  - dist/source.zip
connection: https://cookiejar.{{event_domain}}
service:
  image: ./Dockerfile
  port: 8080
  publish: 31080
  memory: 256Mi
theme_fields:
  location: {city: Amsterdam, lat: 52.37, lon: 4.90}
visible_from: 2026-10-30T18:00:00Z
```

## Field rules

| Field | Rule |
| --- | --- |
| `slug` | Lowercase, digits, dashes; max 64. The import key: importing again updates the challenge with that slug. |
| `description`, hint `content` | If the value is a path to an existing file in the folder, the file is read; otherwise the value is inline Markdown. Rendered to HTML and sanitized (no scripts, no iframes, no inline event handlers). |
| `value` / `scoring` | Exactly one. Dynamic scoring uses `value = max(minimum, ceil(initial - (initial - minimum) * s² / decay²))` with `s = solves - 1` (the common CTF decay curve; the first solver doesn't lower it). Recalculated for every team on each solve; hidden and banned teams don't count. |
| `flags` | At least one. `exact` and `case-insensitive` are stored as hashes (HMAC-SHA256 with the server secret); `pattern` is stored as-is and must compile as a Go RE2 expression, anchored to the full submission. |
| `files` | Must exist; max 100 MB each by default. Stored by SHA-256, so identical files are stored once. |
| `service` | Optional. One shared container for all teams, which an organizer starts from the dashboard: `image` (an image name, or `./Dockerfile` to build from the challenge folder, which is stored with the challenge but never includes `challenge.yml`), `port` inside the container, `publish` (the host port; any free port when left out), `env`, `cpu` (default 0.5, max 4), `memory` (default 256Mi, 16Mi to 8Gi) and `healthcheck` (`tcp` or an HTTP path). See [challenge-services.md](challenge-services.md). Per-team instances arrive in 1.2. |
| `theme_fields` | Values for theme-specific fields, such as a map location. Checked against the active theme's `challengeFields` (the `challengeFields` list in the theme's `theme.json`) by `check`, `import` and the admin upload: a value of the wrong type stops the import; a missing required field is a warning; keys the theme doesn't use are kept for themes that do, with one note per key. |
| `requires` | Unlock chains. Supported by the format from the start; the board UI may show locked challenges later. |

## Commands

| Command | What it does |
| --- | --- |
| `flagfall challenges check <dir or .zip>` | Validates every `challenge.yml` against the schema, then checks that files exist, that a `description` or hint naming a `.md`/`.txt` file has that file, flags aren't empty or redacted, patterns compile, slugs are unique, and `{{...}}` placeholders are known (`{{event_domain}}`, written exactly so). Takes a folder of challenge folders or a single challenge folder. Doesn't touch the database. Exits non-zero on errors, so it can run in CI on a challenge repo. |
| `flagfall challenges import [--yes] [--prune] <dir or .zip>` | Runs `check`, then creates or updates by slug in one transaction. First shows what's new, what changed (and which parts: value, flags, description…), what's unchanged, and which challenges in the database aren't in the folder. Asks before importing; `--yes` skips the question and is required when not run in a terminal (CI, scripts). Never deletes challenges that are missing from the folder unless `--prune` is given: then unsolved ones are deleted and solved ones are retired, so points already scored stay. |
| `flagfall challenges export <dir>` | Writes the same format back out into an empty folder: `challenge.yml`, `description.md`, `hints/N.md` and `dist/` files. Exact flags are stored as hashes and can't be recovered, so they're written as `value: "<redacted: set before import>"`; `import` refuses that placeholder. Retired challenges are written as `state: hidden`; per-team flags are left out. Keep your challenge folder in git as the source of truth. |

The commands use the same configuration as `serve` (`FLAGFALL_DATA_DIR`, `FLAGFALL_DATABASE_URL`), and can run while the server is running: players see the changes at once and the scoreboard catches up within a minute. With Docker, copy the folder into the container first:

```sh
docker compose cp ./my-challenges flagfall:/data/import
docker compose exec flagfall flagfall challenges import --yes /data/import
```

The admin panel offers the same import as a `.zip` upload.

## During a running event

Changing a challenge's value or flags while the event runs is allowed; the import summary warns about it and names the challenges whose flags or points change. A running event is one with a start time that has passed and an end time that hasn't. `import --prune` refuses to remove anything while the event runs: retire challenges in the admin panel instead. Changing a `slug` is treated as a new challenge, so the old one would be pruned.
