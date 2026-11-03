// Package amberphosphor embeds the built Amber Phosphor theme.
// Run `pnpm build` in this folder (or `make web-build`) before `go build`
// to include the real theme; otherwise the server shows a build reminder.
package amberphosphor

import "embed"

// FS holds theme.json and the built dist/ folder.
//
//go:embed theme.json all:dist
var FS embed.FS
