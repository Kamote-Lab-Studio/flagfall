// Package migrations embeds the SQL migrations. See internal/store for how
// dialect tokens in them are expanded before goose runs.
package migrations

import "embed"

// FS holds every *.sql migration, named NNNNN_description.sql.
//
//go:embed *.sql
var FS embed.FS
