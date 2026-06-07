package migrations

import "embed"

// FS contains BoxFleet SQLite schema migrations.
//
//go:embed *.sql
var FS embed.FS
