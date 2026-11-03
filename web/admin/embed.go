// Package admin embeds the built admin panel.
// Run `pnpm build` in this folder (or `make web-build`) before `go build`
// to include it; otherwise /admin shows a build reminder.
package admin

import "embed"

// FS holds the built dist/ folder.
//
//go:embed all:dist
var FS embed.FS
