// Package sample embeds the 12-challenge starter pack used by
// `flagfall seed sample`.
package sample

import "embed"

// FS holds one folder per challenge: challenge.yml, its text, and the files
// players download (dist/). Sources and solutions (src/, solve/) stay in the
// repository only.
//
//go:embed */challenge.yml */*.md */hints */dist
var FS embed.FS
