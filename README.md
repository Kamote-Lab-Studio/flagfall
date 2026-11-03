# Flagfall

**A free, self-hosted CTF platform you can run anywhere and re-skin without code.**

> **Status: pre-alpha (Phase 0).** Nothing here runs an event yet. See [PLAN.md](PLAN.md) for the roadmap and [flagfall-master-plan.md](flagfall-master-plan.md) for the full design.

- **Run anywhere.** One binary or one container. SQLite by default, PostgreSQL for big events. Works offline, so it also runs on air-gapped training networks.
- **Re-skin without code.** Pick a theme in the admin panel, change its colors, logo and wording, preview it on live data, publish. Nine themes are planned for 1.0.
- **Challenges as files.** Each challenge is a `challenge.yml` you can keep in git and import in one step.

## Quick start (developers)

You need [Go 1.26+](https://go.dev/dl/), [Node 22+](https://nodejs.org/) and [pnpm 9](https://pnpm.io/).

```sh
git clone https://github.com/flagfall/flagfall
cd flagfall
pnpm install
make dev            # or: go run ./cmd/flagfall serve
curl localhost:8080/healthz
```

`make test` and `make lint` run everything CI runs. On Windows without `make`, run the `go` and `pnpm` commands from the [Makefile](Makefile) directly.

## Repository layout

| Path | What's there |
| --- | --- |
| `cmd/flagfall/` | Command line: `serve`, `version` (later `backup`, `restore`, `challenges`) |
| `internal/` | Server code |
| `migrations/` | Database migrations |
| `web/sdk/` | Theme SDK, published as `@flagfall/theme-sdk` |
| `web/admin/` | Admin panel |
| `themes/` | Bundled themes; `themes/starter` is the one to copy |
| `challenges/sample/` | 12-challenge starter pack |
| `deploy/` | Dockerfile, Compose, Helm chart, cloud templates |
| `docs/` | Documentation; `docs/dev/` holds design docs |
| `design/` | Static design mockups the themes are built from (not shipped) |
| `e2e/` | Browser tests run against every bundled theme |

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md). Theme pages are a great first contribution. Please follow the [Code of Conduct](CODE_OF_CONDUCT.md), and report security problems privately as described in [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). The Flagfall name and logo are covered by a separate trademark policy (to be published before 1.0).
