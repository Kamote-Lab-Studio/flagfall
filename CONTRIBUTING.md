# Contributing to Flagfall

Thanks for helping. This guide covers how to set up, what we expect in a pull request, and how bigger changes get decided.

## Set up

1. Install Go 1.25+, Node 22+ and pnpm 9.
2. `pnpm install`
3. `make dev` starts the server on http://localhost:8080.
4. `make test` and `make lint` must pass before you open a pull request.

Windows: use Git Bash or WSL for `make`, or run the commands in the [Makefile](Makefile) directly.

## Where to start

- Issues labelled **good first issue**, especially missing theme pages.
- Docs: if something confused you while setting up, a fix to the docs is valuable.

## Pull requests

- Keep each PR to one change. Link the issue it closes.
- Add or update tests. Scoring and flag-checking code needs tests for edge cases.
- Go code is formatted with `gofmt`; TypeScript must pass `tsc --noEmit`.
- UI changes: include screenshots and check keyboard navigation and contrast.
- Write commit messages in the imperative ("Add hint unlock endpoint").

## Bigger changes need a proposal first

Open a thread in GitHub Discussions (category **Proposals**) before writing code for:

- the player API, SSE events or the Theme SDK's public API (`docs/dev/api.md`, `docs/dev/events.md`, `web/sdk/src/`)
- the `theme.json` manifest or `challenge.yml` format
- the database schema
- new event formats or new dependencies

Themes in the wild depend on these, so changes need agreement and a migration path. See [GOVERNANCE.md](GOVERNANCE.md).

## Licensing of contributions

Flagfall is licensed under Apache 2.0. By opening a pull request you agree your contribution is licensed under the same terms (Apache 2.0, section 5). Add a `Signed-off-by` line (`git commit -s`) to certify the [Developer Certificate of Origin](https://developercertificate.org/).

## Security issues

Never in public issues. See [SECURITY.md](SECURITY.md).
