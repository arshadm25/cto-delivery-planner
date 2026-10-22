// Package migrations embeds the PostgreSQL schema migrations. Each version
// has an NNNN_name.up.sql and a matching NNNN_name.down.sql file.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
