// Package starter embeds the built Starter theme, the minimal reference
// theme for theme authors. Run `pnpm build` in this folder (or
// `make web-build`) before `go build` to include it.
package starter

import "embed"

// FS holds theme.json and the built dist/ folder.
//
//go:embed theme.json all:dist
var FS embed.FS
