// Package sample embeds the 12-challenge starter pack used by
// `flagfall seed sample`.
package sample

import "embed"

// FS holds one folder per challenge: challenge.yml, its text, the files
// players download (dist/), and for networked challenges the Dockerfile and
// service/ folder Flagfall builds the container from. Sources and solutions
// (src/, solve/) stay in the repository only.
//
//go:embed */challenge.yml */*.md */hints */dist */Dockerfile */service
var FS embed.FS
