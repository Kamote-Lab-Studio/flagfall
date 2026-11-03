// Package sample embeds the 12-challenge starter pack used by
// `flagfall seed sample`.
package sample

import "embed"

// FS holds one folder per challenge, each with a challenge.yml.
//
//go:embed */challenge.yml */*.md */hints
var FS embed.FS
