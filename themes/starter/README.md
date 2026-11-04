# Starter theme

The smallest complete Flagfall theme: every required page, the theme SDK, live updates, admin preview and settings. It has a plain light design, so you can see the structure. Copy it to build your own theme.

## Layout

```
theme.json            manifest: id, pages, settings (docs/schemas/theme.schema.json)
embed.go              embeds theme.json and dist/ into the Flagfall binary
src/main.ts           mounts the app
src/App.svelte        header, routes, preview banner
src/lib/app.svelte.ts SDK client, shared state, settings, a small router
src/pages/*.svelte    one file per page in theme.json
src/app.css           all styles; colors come from settings as CSS variables
```

## Make your own

1. Copy `themes/starter` to `themes/<your-id>`.
2. Change `id` and `name` in `theme.json`, `name` in `package.json`, `base` in `vite.config.ts` (`/theme/<your-id>/`), and the package name in `embed.go`.
3. Add the package to `internal/themes/bundled/bundled.go`, and add its `package.json` and `dist/` lines to the `Dockerfile` next to the starter's.
4. Build and run the checks:

   ```sh
   pnpm install
   pnpm --filter @flagfall/theme-<your-id> build
   go run ./cmd/flagfall serve                 # publish it under Admin → Appearance
   ```

Then open every page in `theme.json` on the sample event. Each should load without script errors, show an `<h1>`, pass an accessibility check (WCAG 2 A and AA, contrast included), and show keyboard focus.

## Things to keep

- Every page renders an `<h1>`, including when it's loading, empty, or the visitor is signed out.
- Links inside text are underlined: color alone isn't enough.
- Focus outlines stay visible.
- `accent` and `text` declare `contrastWith`/`minContrast`, so the admin warns about unreadable color choices.
